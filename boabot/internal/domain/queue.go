package domain

import "context"

type MessageQueue interface {
	Send(ctx context.Context, queueURL string, msg Message) error
	Receive(ctx context.Context) ([]ReceivedMessage, error)
	Delete(ctx context.Context, receiptHandle string) error
}

type Broadcaster interface {
	Broadcast(ctx context.Context, msg Message) error
}

type ReceivedMessage struct {
	Message       Message
	ReceiptHandle string
}
