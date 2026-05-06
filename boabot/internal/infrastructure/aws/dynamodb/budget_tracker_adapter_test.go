package dynamodb_test

import (
	"context"
	"errors"
	"testing"

	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"

	domaincost "github.com/stainedhead/dev-team-bots/boabot/internal/domain/cost"
	infraddb "github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/dynamodb"
	"github.com/stainedhead/dev-team-bots/boabot/internal/infrastructure/aws/dynamodb/mocks"
)

func defaultCap() domaincost.SystemBudget { return domaincost.DefaultSystemBudget() }

func newAdapter(mock *mocks.DynamoDBClient) *infraddb.BudgetTrackerAdapter {
	bt := newTracker(mock)
	return infraddb.NewBudgetTrackerAdapter(bt, "bot-a", defaultCap(), defaultCap())
}

// ---------------------------------------------------------------------------
// CheckAndRecordToolCall
// ---------------------------------------------------------------------------

func TestAdapter_CheckAndRecordToolCall_RecordsOnSuccess(t *testing.T) {
	mock := &mocks.DynamoDBClient{}
	a := newAdapter(mock)

	if err := a.CheckAndRecordToolCall(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CheckBudget calls GetItem; RecordSpend calls UpdateItem
	if len(mock.UpdateItemCalls) != 1 {
		t.Fatalf("expected 1 UpdateItem call (RecordSpend), got %d", len(mock.UpdateItemCalls))
	}
}

func TestAdapter_CheckAndRecordToolCall_PropagatesCheckError(t *testing.T) {
	checkErr := errors.New("dynamo get failed")
	mock := &mocks.DynamoDBClient{
		GetItemFn: func(_ context.Context, _ *awsdynamodb.GetItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
			return nil, checkErr
		},
	}
	a := newAdapter(mock)

	err := a.CheckAndRecordToolCall(context.Background())
	if err == nil {
		t.Fatal("expected error from CheckBudget, got nil")
	}
	// RecordSpend should NOT have been called when budget check fails.
	if len(mock.UpdateItemCalls) != 0 {
		t.Fatalf("expected 0 UpdateItem calls when check fails, got %d", len(mock.UpdateItemCalls))
	}
}

func TestAdapter_CheckAndRecordToolCall_PropagatesRecordError(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		UpdateItemFn: func(_ context.Context, _ *awsdynamodb.UpdateItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.UpdateItemOutput, error) {
			return nil, errors.New("dynamo update failed")
		},
	}
	a := newAdapter(mock)

	if err := a.CheckAndRecordToolCall(context.Background()); err == nil {
		t.Fatal("expected error from RecordSpend, got nil")
	}
}

// ---------------------------------------------------------------------------
// CheckAndRecordTokens
// ---------------------------------------------------------------------------

func TestAdapter_CheckAndRecordTokens_RecordsOnSuccess(t *testing.T) {
	mock := &mocks.DynamoDBClient{}
	a := newAdapter(mock)

	if err := a.CheckAndRecordTokens(context.Background(), 500); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.UpdateItemCalls) != 1 {
		t.Fatalf("expected 1 UpdateItem call (RecordSpend), got %d", len(mock.UpdateItemCalls))
	}
}

func TestAdapter_CheckAndRecordTokens_PropagatesCheckError(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		GetItemFn: func(_ context.Context, _ *awsdynamodb.GetItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
			return nil, errors.New("get failed")
		},
	}
	a := newAdapter(mock)

	if err := a.CheckAndRecordTokens(context.Background(), 100); err == nil {
		t.Fatal("expected error from CheckBudget, got nil")
	}
	if len(mock.UpdateItemCalls) != 0 {
		t.Fatalf("expected 0 UpdateItem calls when check fails, got %d", len(mock.UpdateItemCalls))
	}
}

func TestAdapter_CheckAndRecordTokens_PropagatesRecordError(t *testing.T) {
	mock := &mocks.DynamoDBClient{
		UpdateItemFn: func(_ context.Context, _ *awsdynamodb.UpdateItemInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.UpdateItemOutput, error) {
			return nil, errors.New("update failed")
		},
	}
	a := newAdapter(mock)

	if err := a.CheckAndRecordTokens(context.Background(), 100); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Flush — no-op
// ---------------------------------------------------------------------------

func TestAdapter_Flush_IsNoOp(t *testing.T) {
	mock := &mocks.DynamoDBClient{}
	a := newAdapter(mock)

	if err := a.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: expected no error, got %v", err)
	}
	if len(mock.UpdateItemCalls)+len(mock.GetItemCalls)+len(mock.PutItemCalls) != 0 {
		t.Fatal("Flush should make no DynamoDB calls")
	}
}
