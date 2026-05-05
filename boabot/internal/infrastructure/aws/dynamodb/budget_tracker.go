// Package dynamodb provides a DynamoDB-backed implementation of the cost
// enforcement interfaces defined in the domain/cost package.
package dynamodb

import (
	"context"
	"fmt"
	"strconv"
	"time"

	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	domaincost "github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
)

// DynamoDBClient is the subset of the AWS DynamoDB SDK client that
// BudgetTracker requires. This interface enables test injection.
type DynamoDBClient interface {
	GetItem(ctx context.Context, params *awsdynamodb.GetItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, params *awsdynamodb.PutItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.PutItemOutput, error)
	UpdateItem(ctx context.Context, params *awsdynamodb.UpdateItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.UpdateItemOutput, error)
	Query(ctx context.Context, params *awsdynamodb.QueryInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error)
}

// BudgetTracker persists per-bot daily spend in DynamoDB and enforces cost
// caps defined in domaincost.SystemBudget.
//
// Table schema:
//   - Partition key: botId  (S)
//   - Sort key:      date   (S, format YYYYMMDD)
//   - Attributes:    tokenSpend (N), toolCallCount (N), usdSpend (N)
type BudgetTracker struct {
	client    DynamoDBClient
	tableName string
	now       func() time.Time // injectable for tests
}

// NewBudgetTracker constructs a BudgetTracker targeting tableName.
func NewBudgetTracker(client DynamoDBClient, tableName string) *BudgetTracker {
	return &BudgetTracker{client: client, tableName: tableName, now: time.Now}
}

func (bt *BudgetTracker) today() string { return bt.now().UTC().Format("20060102") }
func (bt *BudgetTracker) month() string { return bt.now().UTC().Format("200601") }

// RecordSpend atomically increments the spend counters for botID on today's date.
func (bt *BudgetTracker) RecordSpend(ctx context.Context, botID string, tokens int64, toolCalls int, usdSpend float64) error {
	_, err := bt.client.UpdateItem(ctx, &awsdynamodb.UpdateItemInput{
		TableName: &bt.tableName,
		Key: map[string]types.AttributeValue{
			"botId": &types.AttributeValueMemberS{Value: botID},
			"date":  &types.AttributeValueMemberS{Value: bt.today()},
		},
		UpdateExpression: strPtr("ADD tokenSpend :t, toolCallCount :c, usdSpend :u"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":t": &types.AttributeValueMemberN{Value: strconv.FormatInt(tokens, 10)},
			":c": &types.AttributeValueMemberN{Value: strconv.Itoa(toolCalls)},
			":u": &types.AttributeValueMemberN{Value: strconv.FormatFloat(usdSpend, 'f', 10, 64)},
		},
	})
	return err
}

// DailySpend returns the USD spend for botID today.
func (bt *BudgetTracker) DailySpend(ctx context.Context, botID string) (float64, error) {
	out, err := bt.client.GetItem(ctx, &awsdynamodb.GetItemInput{
		TableName: &bt.tableName,
		Key: map[string]types.AttributeValue{
			"botId": &types.AttributeValueMemberS{Value: botID},
			"date":  &types.AttributeValueMemberS{Value: bt.today()},
		},
	})
	if err != nil {
		return 0, err
	}
	return parseN(out.Item, "usdSpend"), nil
}

// MonthlySpend returns the total USD spend across all bots for the current month.
func (bt *BudgetTracker) MonthlySpend(ctx context.Context) (float64, error) {
	month := bt.month()
	out, err := bt.client.Query(ctx, &awsdynamodb.QueryInput{
		TableName:              &bt.tableName,
		IndexName:              strPtr("date-index"),
		KeyConditionExpression: strPtr("begins_with(#d, :m)"),
		ExpressionAttributeNames:  map[string]string{"#d": "date"},
		ExpressionAttributeValues: map[string]types.AttributeValue{":m": &types.AttributeValueMemberS{Value: month}},
	})
	if err != nil {
		return 0, err
	}
	var total float64
	for _, item := range out.Items {
		total += parseN(item, "usdSpend")
	}
	return total, nil
}

// SetNow overrides the clock used by BudgetTracker. Used only in tests.
func (bt *BudgetTracker) SetNow(fn func() time.Time) { bt.now = fn }

// CheckBudget returns an error if adding the proposed spend would breach either
// the per-bot daily cap or the system-wide monthly cap.
func (bt *BudgetTracker) CheckBudget(ctx context.Context, botID string, tokens int64, toolCalls int, usdSpend float64, perBotCap, systemBudget domaincost.SystemBudget) error {
	daily, err := bt.DailySpend(ctx, botID)
	if err != nil {
		return fmt.Errorf("check budget: daily spend: %w", err)
	}
	if perBotCap.SystemDailyCapUSD > 0 && daily+usdSpend > perBotCap.SystemDailyCapUSD {
		return &domaincost.BudgetExceededError{
			BotID:   domaincost.BotID(botID),
			Reason:  "bot daily cap exceeded",
			Current: daily + usdSpend,
			Cap:     perBotCap.SystemDailyCapUSD,
		}
	}

	monthly, err := bt.MonthlySpend(ctx)
	if err != nil {
		return fmt.Errorf("check budget: monthly spend: %w", err)
	}
	if systemBudget.SystemMonthlyCapUSD > 0 && monthly+usdSpend > systemBudget.SystemMonthlyCapUSD {
		return &domaincost.BudgetExceededError{
			BotID:   domaincost.BotID(botID),
			Reason:  "system monthly cap exceeded",
			Current: monthly + usdSpend,
			Cap:     systemBudget.SystemMonthlyCapUSD,
		}
	}
	return nil
}

// DailySpikeAlert returns true if today's spend for botID exceeds
// (monthlyCapUSD/daysInMonth) * (1 + spikeThresholdPct).
func (bt *BudgetTracker) DailySpikeAlert(ctx context.Context, botID string, budget domaincost.SystemBudget) (bool, error) {
	daily, err := bt.DailySpend(ctx, botID)
	if err != nil {
		return false, err
	}
	now := bt.now().UTC()
	daysInMonth := float64(daysIn(now.Year(), now.Month()))
	proRated := budget.SystemMonthlyCapUSD / daysInMonth
	threshold := proRated * (1 + budget.SpikeAlertThresholdPct)
	return daily > threshold, nil
}

// FlatCapAlert returns true if total monthly spend has exceeded
// monthlyCapUSD * flatCapThresholdPct.
func (bt *BudgetTracker) FlatCapAlert(ctx context.Context, budget domaincost.SystemBudget) (bool, error) {
	monthly, err := bt.MonthlySpend(ctx)
	if err != nil {
		return false, err
	}
	threshold := budget.SystemMonthlyCapUSD * budget.FlatCapAlertThresholdPct
	return monthly > threshold, nil
}

// --- helpers -----------------------------------------------------------------

func parseN(item map[string]types.AttributeValue, key string) float64 {
	v, ok := item[key]
	if !ok {
		return 0
	}
	n, ok := v.(*types.AttributeValueMemberN)
	if !ok {
		return 0
	}
	f, _ := strconv.ParseFloat(n.Value, 64)
	return f
}

func strPtr(s string) *string { return &s }

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
