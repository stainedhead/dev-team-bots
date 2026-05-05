package dynamodb_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	domaincost "github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
	infraddb "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/dynamodb"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/dynamodb/mocks"
)

func fixedNow() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) }

func newTracker(mock *mocks.DynamoDBClient) *infraddb.BudgetTracker {
	bt := infraddb.NewBudgetTracker(mock, "baobotBudget")
	bt.SetNow(fixedNow)
	return bt
}

func nVal(v float64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatFloat(v, 'f', 10, 64)}
}

func TestRecordSpend_CallsUpdateItem(t *testing.T) {
	mock := &mocks.DynamoDBClient{}
	bt := newTracker(mock)

	if err := bt.RecordSpend(context.Background(), "bot-a", 1000, 5, 0.12); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.UpdateItemCalls) != 1 {
		t.Fatalf("expected 1 UpdateItem call, got %d", len(mock.UpdateItemCalls))
	}
}

func TestRecordSpend_PropagatesError(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		UpdateItemFn: func(_ context.Context, _ *awsdynamodb.UpdateItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.UpdateItemOutput, error) {
			return nil, &types.ResourceNotFoundException{Message: strPtr("table not found")}
		},
	}
	bt := newTracker(mock)
	if err := bt.RecordSpend(context.Background(), "bot-a", 100, 1, 0.01); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDailySpend_ReturnsUSDSpend(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		GetItemFn: func(_ context.Context, _ *awsdynamodb.GetItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
			return &awsdynamodb.GetItemOutput{
				Item: map[string]types.AttributeValue{
					"usdSpend": nVal(1.5),
				},
			}, nil
		},
	}
	bt := newTracker(mock)
	spend, err := bt.DailySpend(context.Background(), "bot-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spend != 1.5 {
		t.Errorf("expected 1.5, got %v", spend)
	}
}

func TestDailySpend_MissingItem_ReturnsZero(t *testing.T) {
	mock := &mocks.DynamoDBClient{} // returns empty GetItemOutput
	bt := newTracker(mock)
	spend, err := bt.DailySpend(context.Background(), "bot-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spend != 0 {
		t.Errorf("expected 0, got %v", spend)
	}
}

func TestMonthlySpend_SumsAcrossItems(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		QueryFn: func(_ context.Context, _ *awsdynamodb.QueryInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error) {
			return &awsdynamodb.QueryOutput{
				Items: []map[string]types.AttributeValue{
					{"usdSpend": nVal(2.0)},
					{"usdSpend": nVal(3.5)},
				},
			}, nil
		},
	}
	bt := newTracker(mock)
	total, err := bt.MonthlySpend(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 5.5 {
		t.Errorf("expected 5.5, got %v", total)
	}
}

func TestCheckBudget_BotDailyCapExceeded(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		GetItemFn: func(_ context.Context, _ *awsdynamodb.GetItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
			return &awsdynamodb.GetItemOutput{
				Item: map[string]types.AttributeValue{"usdSpend": nVal(9.5)},
			}, nil
		},
		QueryFn: func(_ context.Context, _ *awsdynamodb.QueryInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error) {
			return &awsdynamodb.QueryOutput{}, nil
		},
	}
	bt := newTracker(mock)

	perBot := domaincost.SystemBudget{SystemDailyCapUSD: 10.0}
	sys := domaincost.SystemBudget{SystemMonthlyCapUSD: 300.0}

	err := bt.CheckBudget(context.Background(), "bot-a", 0, 0, 1.0, perBot, sys)
	if err == nil {
		t.Fatal("expected BudgetExceededError, got nil")
	}
	var budgetErr *domaincost.BudgetExceededError
	if !isBudgetExceeded(err, &budgetErr) {
		t.Fatalf("expected *BudgetExceededError, got %T: %v", err, err)
	}
	if budgetErr.Reason != "bot daily cap exceeded" {
		t.Errorf("unexpected reason: %s", budgetErr.Reason)
	}
}

func TestCheckBudget_SystemMonthlyCap(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		GetItemFn: func(_ context.Context, _ *awsdynamodb.GetItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
			return &awsdynamodb.GetItemOutput{
				Item: map[string]types.AttributeValue{"usdSpend": nVal(0.0)},
			}, nil
		},
		QueryFn: func(_ context.Context, _ *awsdynamodb.QueryInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error) {
			return &awsdynamodb.QueryOutput{
				Items: []map[string]types.AttributeValue{
					{"usdSpend": nVal(299.0)},
				},
			}, nil
		},
	}
	bt := newTracker(mock)

	perBot := domaincost.SystemBudget{SystemDailyCapUSD: 50.0}
	sys := domaincost.SystemBudget{SystemMonthlyCapUSD: 300.0}

	err := bt.CheckBudget(context.Background(), "bot-a", 0, 0, 5.0, perBot, sys)
	if err == nil {
		t.Fatal("expected BudgetExceededError for monthly cap")
	}
}

func TestCheckBudget_UnderLimits_NoError(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		GetItemFn: func(_ context.Context, _ *awsdynamodb.GetItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
			return &awsdynamodb.GetItemOutput{
				Item: map[string]types.AttributeValue{"usdSpend": nVal(1.0)},
			}, nil
		},
		QueryFn: func(_ context.Context, _ *awsdynamodb.QueryInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error) {
			return &awsdynamodb.QueryOutput{
				Items: []map[string]types.AttributeValue{{"usdSpend": nVal(10.0)}},
			}, nil
		},
	}
	bt := newTracker(mock)
	perBot := domaincost.SystemBudget{SystemDailyCapUSD: 50.0}
	sys := domaincost.SystemBudget{SystemMonthlyCapUSD: 300.0}
	if err := bt.CheckBudget(context.Background(), "bot-a", 0, 0, 1.0, perBot, sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDailySpikeAlert_FiredAt130Pct(t *testing.T) {
	// monthly=300, days=31, proRated≈9.677, threshold at 30% spike = 12.58
	mock := &mocks.DynamoDBClient{
		GetItemFn: func(_ context.Context, _ *awsdynamodb.GetItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
			return &awsdynamodb.GetItemOutput{
				Item: map[string]types.AttributeValue{"usdSpend": nVal(13.0)},
			}, nil
		},
	}
	bt := newTracker(mock)
	budget := domaincost.SystemBudget{SystemMonthlyCapUSD: 300.0, SpikeAlertThresholdPct: 0.30}
	fired, err := bt.DailySpikeAlert(context.Background(), "bot-a", budget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fired {
		t.Error("expected spike alert to fire")
	}
}

func TestDailySpikeAlert_NotFired(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		GetItemFn: func(_ context.Context, _ *awsdynamodb.GetItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
			return &awsdynamodb.GetItemOutput{
				Item: map[string]types.AttributeValue{"usdSpend": nVal(5.0)},
			}, nil
		},
	}
	bt := newTracker(mock)
	budget := domaincost.SystemBudget{SystemMonthlyCapUSD: 300.0, SpikeAlertThresholdPct: 0.30}
	fired, err := bt.DailySpikeAlert(context.Background(), "bot-a", budget)
	if err != nil || fired {
		t.Errorf("expected no spike alert; fired=%v err=%v", fired, err)
	}
}

func TestFlatCapAlert_FiredAt80Pct(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		QueryFn: func(_ context.Context, _ *awsdynamodb.QueryInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error) {
			return &awsdynamodb.QueryOutput{
				Items: []map[string]types.AttributeValue{{"usdSpend": nVal(245.0)}},
			}, nil
		},
	}
	bt := newTracker(mock)
	budget := domaincost.SystemBudget{SystemMonthlyCapUSD: 300.0, FlatCapAlertThresholdPct: 0.80}
	fired, err := bt.FlatCapAlert(context.Background(), budget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fired {
		t.Error("expected flat cap alert to fire at 245/300 (81.7%)")
	}
}

func TestFlatCapAlert_NotFired(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		QueryFn: func(_ context.Context, _ *awsdynamodb.QueryInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error) {
			return &awsdynamodb.QueryOutput{
				Items: []map[string]types.AttributeValue{{"usdSpend": nVal(100.0)}},
			}, nil
		},
	}
	bt := newTracker(mock)
	budget := domaincost.SystemBudget{SystemMonthlyCapUSD: 300.0, FlatCapAlertThresholdPct: 0.80}
	fired, err := bt.FlatCapAlert(context.Background(), budget)
	if err != nil || fired {
		t.Errorf("expected no flat cap alert; fired=%v err=%v", fired, err)
	}
}

// helpers
func strPtr(s string) *string { return &s }

func isBudgetExceeded(err error, out **domaincost.BudgetExceededError) bool {
	if e, ok := err.(*domaincost.BudgetExceededError); ok {
		*out = e
		return true
	}
	return false
}
