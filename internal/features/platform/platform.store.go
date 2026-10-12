package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"familytime/backend/internal/features/wallet"
	"familytime/backend/internal/games/quiz"
	"familytime/backend/internal/lib/logger"
	"familytime/backend/internal/lib/result"
	"familytime/backend/internal/lib/vatar"
	"familytime/backend/internal/shared/constants"
	"familytime/backend/internal/shared/messages"
)

const (
	webhookSignatureHeader = "X-Vatar-Signature"
	maxWebhookBytes        = 64 << 10
)

// soloRun ties a solo room to the platform player it was opened for, so a
// reveal can only ever be bought for your own game.
type soloRun struct {
	platformID string
	playerID   string
}

func unavailable(reason string) *result.Failure {
	return &result.Failure{Code: constants.CodePlatformUnavailable, MessageKey: messages.PlatformUnavailable, Status: constants.StatusBadGateway, RejectionReason: reason}
}

func products() []ProductView {
	out := make([]ProductView, 0, len(wallet.Products))
	for id, gems := range wallet.Products {
		out = append(out, ProductView{ID: id, Gems: gems})
	}
	return out
}

// Wallet is a player's gem balance and the packs on sale.
func (s *Service) Wallet(ctx context.Context, token string) result.Result[WalletView] {
	session, failure := s.identify(ctx, token)
	if failure != nil {
		return result.FailWith[WalletView](failure)
	}
	gems, err := s.wallets.Balance(ctx, session.PlayerID)
	if err != nil {
		logger.With(ctx).Error("wallet read failed", "error", err.Error())
		return result.Fail[WalletView](constants.CodeInternal, messages.CommonInternal, constants.StatusInternal)
	}
	return result.Ok(WalletView{Gems: gems, Products: products()})
}

// ClaimPurchase turns a paid platform purchase into gems. The game saying
// "paid" proves nothing: the platform is asked, the purchase must belong to
// the player asking, the gems are given once per purchase id, and only then
// is it marked delivered.
func (s *Service) ClaimPurchase(ctx context.Context, token, purchaseID string) result.Result[ClaimView] {
	session, failure := s.identify(ctx, token)
	if failure != nil {
		return result.FailWith[ClaimView](failure)
	}
	return s.settlePurchase(ctx, purchaseID, session.PlayerID)
}

// settlePurchase is the one path a purchase becomes gems by, shared by the
// game's claim and the platform's webhook. claimant is the player asking, or
// empty when the platform itself is telling us (the webhook).
func (s *Service) settlePurchase(ctx context.Context, purchaseID, claimant string) result.Result[ClaimView] {
	purchase, err := s.platform.GetPurchase(ctx, purchaseID)
	var apiErr *vatar.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
		return result.Fail[ClaimView](constants.CodePurchaseNotFound, messages.StorePurchaseNotFound, constants.StatusNotFound)
	case err != nil:
		logger.With(ctx).Error("could not read platform purchase", "purchase_id", purchaseID, "error", err.Error())
		return result.FailWith[ClaimView](unavailable("purchase_fetch_failed"))
	}

	owner := purchase.PlayerID
	if s.platform.DevMode() {
		// A stand-in purchase has no owner on record.
		owner = claimant
	}
	if owner == "" || (claimant != "" && owner != claimant) {
		return result.Fail[ClaimView](constants.CodePurchaseNotFound, messages.StorePurchaseNotFound, constants.StatusNotFound)
	}
	if purchase.Status == vatar.PurchaseRefunded {
		return result.Fail[ClaimView](constants.CodePurchaseRefunded, messages.StorePurchaseRefunded, constants.StatusConflict)
	}
	gems, known := wallet.Products[purchase.ProductID]
	if !known {
		logger.With(ctx).Error("purchase of a product this server does not sell", "purchase_id", purchaseID, "product_id", purchase.ProductID)
		return result.Fail[ClaimView](constants.CodePurchaseNotFound, messages.StorePurchaseNotFound, constants.StatusNotFound)
	}

	credited, balance, err := s.wallets.Credit(ctx, wallet.Purchase{ID: purchase.ID, PlayerID: owner, ProductID: purchase.ProductID, Gems: gems, CreatedAt: time.Now().UTC()})
	if err != nil {
		logger.With(ctx).Error("could not credit purchase", "purchase_id", purchaseID, "error", err.Error())
		return result.Fail[ClaimView](constants.CodeInternal, messages.CommonInternal, constants.StatusInternal)
	}
	// Delivered only after the gems are safely in the wallet. Marking it again
	// for an already-credited purchase is harmless, and finishes the job if an
	// earlier attempt stopped between the two steps.
	if err := s.platform.DeliverPurchase(ctx, purchase.ID); err != nil {
		logger.With(ctx).Error("could not mark purchase delivered", "purchase_id", purchaseID, "error", err.Error())
		return result.FailWith[ClaimView](unavailable("purchase_deliver_failed"))
	}
	if credited {
		logger.With(ctx).Info("purchase delivered", "purchase_id", purchaseID, "product_id", purchase.ProductID, "gems", gems)
	}
	return result.Ok(ClaimView{Gems: balance, Credited: credited, Added: gems})
}

// Reveal shows a solo player the answer to the question in front of them, for
// gems. The gems are taken first and given back if the reveal then cannot
// happen (the clock ran out in between), so a player never pays for nothing.
func (s *Service) Reveal(ctx context.Context, token, code string, questionSeq int) result.Result[RevealView] {
	session, failure := s.identify(ctx, token)
	if failure != nil {
		return result.FailWith[RevealView](failure)
	}

	s.mu.Lock()
	run, tracked := s.solos[code]
	s.mu.Unlock()
	room, open := s.registry.Get(code)
	if !tracked || !open {
		s.mu.Lock()
		delete(s.solos, code)
		s.mu.Unlock()
		return result.Fail[RevealView](constants.CodeRoomNotFound, messages.RoomsNotFound, constants.StatusNotFound)
	}
	if run.platformID != session.PlayerID {
		return result.Fail[RevealView](constants.CodeNotHost, messages.StoreNotYourGame, constants.StatusForbidden)
	}

	notNow := result.Fail[RevealView](constants.CodeInvalidAction, messages.PlayInvalidAction, constants.StatusUnprocessable)
	view, ok := room.PlayerView(run.playerID).(quiz.View)
	if !ok || view.RevealCost <= 0 || view.QuestionSeq != questionSeq {
		return notNow
	}
	cost := view.RevealCost

	paid, balance, err := s.wallets.Spend(ctx, session.PlayerID, cost)
	if err != nil {
		logger.With(ctx).Error("wallet spend failed", "error", err.Error())
		return result.Fail[RevealView](constants.CodeInternal, messages.CommonInternal, constants.StatusInternal)
	}
	if !paid {
		return result.Fail[RevealView](constants.CodeNotEnoughGems, messages.StoreNotEnoughGems, constants.StatusConflict)
	}

	payload, _ := json.Marshal(map[string]int{"question_seq": questionSeq})
	if failure := room.ServerAction(run.playerID, quiz.ActionReveal, payload); failure != nil {
		if _, err := s.wallets.Refund(ctx, session.PlayerID, cost); err != nil {
			logger.With(ctx).Error("could not refund a failed reveal", "player", session.PlayerID, "gems", cost, "error", err.Error())
		}
		return result.FailWith[RevealView](failure)
	}

	answer := -1
	if after, ok := room.PlayerView(run.playerID).(quiz.View); ok && after.You != nil && after.You.HintIdx != nil {
		answer = *after.You.HintIdx
	}
	return result.Ok(RevealView{AnswerIdx: answer, Cost: cost, Gems: balance})
}

// Webhook handles a signed nudge from the platform. A paid purchase is
// settled here too, so the gems arrive even if the player's phone died on the
// way back from paying. It is only ever a nudge: the purchase is still checked
// with the platform before anything is given.
func (s *Service) Webhook(ctx context.Context, rawBody []byte, signature string) result.Result[struct{}] {
	if !vatar.WebhookSigned(s.webhookSecret, rawBody, signature) {
		return result.Fail[struct{}](constants.CodeBadSignature, messages.StoreBadSignature, constants.StatusUnauthorized)
	}
	var event struct {
		Type string `json:"type"`
		Data struct {
			PurchaseID string `json:"purchaseId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rawBody, &event); err != nil {
		return result.Fail[struct{}](constants.CodeMalformedJSON, messages.CommonMalformedJSON, constants.StatusBadRequest)
	}
	if event.Type != vatar.EventPurchasePaid || event.Data.PurchaseID == "" {
		return result.Ok(struct{}{}) // match.started, match.settled: nothing to do
	}

	settled := s.settlePurchase(ctx, event.Data.PurchaseID, "")
	// A purchase the platform no longer has (a test event's made-up id) is
	// not a reason to make them retry. A fault on our side or theirs is.
	if !settled.OK() && settled.Failure.Code != constants.CodePurchaseNotFound && settled.Failure.Code != constants.CodePurchaseRefunded {
		return result.FailWith[struct{}](settled.Failure)
	}
	return result.Ok(struct{}{})
}
