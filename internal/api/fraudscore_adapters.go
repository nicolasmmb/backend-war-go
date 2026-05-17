package api

import (
	"backend/internal/application/fraudscore"
	"backend/internal/domain/fraud"
	"backend/internal/ivf"
)

type apiVectorizerAdapter struct{}

func (apiVectorizerAdapter) Vectorize(req *fraud.Request) (fraud.FeatureVector, error) {
	httpReq := mapToTransportFraudRequest(req)
	query, err := Vectorize(&httpReq)
	if err != nil {
		return fraud.FeatureVector{}, err
	}
	return fraud.FeatureVector(query), nil
}

type ivfSearcherAdapter struct {
	ds     *ivf.Dataset
	nprobe int
}

func newIVFSearcherAdapter(ds *ivf.Dataset, nprobe int) ivfSearcherAdapter {
	if nprobe <= 0 {
		nprobe = defaultNProbe
	}
	return ivfSearcherAdapter{ds: ds, nprobe: nprobe}
}

func (s ivfSearcherAdapter) FraudCount(query *fraud.FeatureVector) uint8 {
	ivfQuery := [ivf.Dim]float32(*query)
	return SearchFraudCount(&ivfQuery, s.ds, s.nprobe)
}

func newFraudScoreUseCase(ds *ivf.Dataset, nprobe int) (*fraudscore.UseCase, error) {
	return fraudscore.NewUseCase(
		apiVectorizerAdapter{},
		newIVFSearcherAdapter(ds, nprobe),
	)
}

func mapToDomainFraudRequest(req *FraudRequest) fraud.Request {
	if req == nil {
		return fraud.Request{}
	}

	domainReq := fraud.Request{
		ID: req.ID,
		Transaction: fraud.Transaction{
			Amount:       req.Transaction.Amount,
			Installments: req.Transaction.Installments,
			RequestedAt:  req.Transaction.RequestedAt,
		},
		Customer: fraud.Customer{
			AvgAmount:      req.Customer.AvgAmount,
			TxCount24h:     req.Customer.TxCount24h,
			KnownMerchants: req.Customer.KnownMerchants,
		},
		Merchant: fraud.Merchant{
			ID:        req.Merchant.ID,
			MCC:       req.Merchant.MCC,
			AvgAmount: req.Merchant.AvgAmount,
		},
		Terminal: fraud.Terminal{
			IsOnline:    req.Terminal.IsOnline,
			CardPresent: req.Terminal.CardPresent,
			KmFromHome:  req.Terminal.KmFromHome,
		},
	}

	if req.LastTx != nil {
		domainReq.LastTx = &fraud.LastTransaction{
			Timestamp:     req.LastTx.Timestamp,
			KmFromCurrent: req.LastTx.KmFromCurrent,
		}
	}

	return domainReq
}

func mapToTransportFraudRequest(req *fraud.Request) FraudRequest {
	if req == nil {
		return FraudRequest{}
	}

	httpReq := FraudRequest{
		ID: req.ID,
		Transaction: TransactionData{
			Amount:       req.Transaction.Amount,
			Installments: req.Transaction.Installments,
			RequestedAt:  req.Transaction.RequestedAt,
		},
		Customer: CustomerData{
			AvgAmount:      req.Customer.AvgAmount,
			TxCount24h:     req.Customer.TxCount24h,
			KnownMerchants: req.Customer.KnownMerchants,
		},
		Merchant: MerchantData{
			ID:        req.Merchant.ID,
			MCC:       req.Merchant.MCC,
			AvgAmount: req.Merchant.AvgAmount,
		},
		Terminal: TerminalData{
			IsOnline:    req.Terminal.IsOnline,
			CardPresent: req.Terminal.CardPresent,
			KmFromHome:  req.Terminal.KmFromHome,
		},
	}

	if req.LastTx != nil {
		httpReq.LastTx = &LastTxData{
			Timestamp:     req.LastTx.Timestamp,
			KmFromCurrent: req.LastTx.KmFromCurrent,
		}
	}

	return httpReq
}
