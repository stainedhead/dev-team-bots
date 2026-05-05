// Package mocks provides hand-written test doubles for the DynamoDB
// infrastructure interfaces.
package mocks

import (
	"context"

	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// DynamoDBClient is a hand-written mock of the dynamodb.DynamoDBClient interface.
type DynamoDBClient struct {
	GetItemFn    func(ctx context.Context, params *awsdynamodb.GetItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error)
	PutItemFn    func(ctx context.Context, params *awsdynamodb.PutItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.PutItemOutput, error)
	UpdateItemFn func(ctx context.Context, params *awsdynamodb.UpdateItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.UpdateItemOutput, error)
	QueryFn      func(ctx context.Context, params *awsdynamodb.QueryInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error)

	GetItemCalls    []*awsdynamodb.GetItemInput
	PutItemCalls    []*awsdynamodb.PutItemInput
	UpdateItemCalls []*awsdynamodb.UpdateItemInput
	QueryCalls      []*awsdynamodb.QueryInput
}

func (m *DynamoDBClient) GetItem(ctx context.Context, params *awsdynamodb.GetItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error) {
	m.GetItemCalls = append(m.GetItemCalls, params)
	if m.GetItemFn != nil {
		return m.GetItemFn(ctx, params, optFns...)
	}
	return &awsdynamodb.GetItemOutput{}, nil
}

func (m *DynamoDBClient) PutItem(ctx context.Context, params *awsdynamodb.PutItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.PutItemOutput, error) {
	m.PutItemCalls = append(m.PutItemCalls, params)
	if m.PutItemFn != nil {
		return m.PutItemFn(ctx, params, optFns...)
	}
	return &awsdynamodb.PutItemOutput{}, nil
}

func (m *DynamoDBClient) UpdateItem(ctx context.Context, params *awsdynamodb.UpdateItemInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.UpdateItemOutput, error) {
	m.UpdateItemCalls = append(m.UpdateItemCalls, params)
	if m.UpdateItemFn != nil {
		return m.UpdateItemFn(ctx, params, optFns...)
	}
	return &awsdynamodb.UpdateItemOutput{}, nil
}

func (m *DynamoDBClient) Query(ctx context.Context, params *awsdynamodb.QueryInput, optFns ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error) {
	m.QueryCalls = append(m.QueryCalls, params)
	if m.QueryFn != nil {
		return m.QueryFn(ctx, params, optFns...)
	}
	return &awsdynamodb.QueryOutput{}, nil
}
