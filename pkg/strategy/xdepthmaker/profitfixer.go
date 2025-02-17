package xdepthmaker

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/c9s/bbgo/pkg/exchange/batch"
	"github.com/c9s/bbgo/pkg/types"
)

type ProfitFixerConfig struct {
	TradesSince types.Time `json:"tradesSince,omitempty"`
}

// ProfitFixer implements a trade history based profit fixer
type ProfitFixer struct {
	market types.Market

	sessions map[string]types.ExchangeTradeHistoryService
}

func NewProfitFixer(market types.Market) *ProfitFixer {
	return &ProfitFixer{
		market:   market,
		sessions: make(map[string]types.ExchangeTradeHistoryService),
	}
}

func (f *ProfitFixer) AddExchange(sessionName string, service types.ExchangeTradeHistoryService) {
	f.sessions[sessionName] = service
}

func (f *ProfitFixer) batchQueryTrades(
	ctx context.Context,
	service types.ExchangeTradeHistoryService,
	symbol string,
	since, until time.Time,
) ([]types.Trade, error) {
	q := &batch.TradeBatchQuery{ExchangeTradeHistoryService: service}
	return q.QueryTrades(ctx, symbol, &types.TradeQueryOptions{
		StartTime: &since,
		EndTime:   &until,
	})
}

func (f *ProfitFixer) Fix(ctx context.Context, since, until time.Time, stats *types.ProfitStats, position *types.Position) error {
	var mu sync.Mutex
	var allTrades = make([]types.Trade, 0, 1000)

	g, subCtx := errgroup.WithContext(ctx)
	for n, s := range f.sessions {
		// allocate a copy of the iteration variables
		sessionName := n
		service := s
		g.Go(func() error {
			log.Infof("batch querying %s trade history from %s since %s until %s", f.market.Symbol, sessionName, since.String(), until.String())
			trades, err := f.batchQueryTrades(subCtx, service, f.market.Symbol, since, until)
			if err != nil {
				log.WithError(err).Errorf("unable to batch query trades for fixer")
				return err
			}

			mu.Lock()
			allTrades = append(allTrades, trades...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	allTrades = types.SortTradesAscending(allTrades)
	for _, trade := range allTrades {
		stats.AddTrade(trade)
		position.AddTrade(trade)
	}

	return nil
}
