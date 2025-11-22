package workers

import (
	"context"
	"time"

	"constellation-overwatch/pkg/services/logger"
	"github.com/nats-io/nats.go"
)

type Worker interface {
	Start(ctx context.Context) error
	Stop() error
	Name() string
}

type BaseWorker struct {
	name     string
	nc       *nats.Conn
	js       nats.JetStreamContext
	sub      *nats.Subscription
	consumer string
	stream   string
	subject  string
}

func NewBaseWorker(name string, nc *nats.Conn, js nats.JetStreamContext, stream, consumer, subject string) *BaseWorker {
	return &BaseWorker{
		name:     name,
		nc:       nc,
		js:       js,
		consumer: consumer,
		stream:   stream,
		subject:  subject,
	}
}

func (w *BaseWorker) Name() string {
	return w.name
}

func (w *BaseWorker) Stop() error {
	if w.sub != nil {
		return w.sub.Drain()
	}
	return nil
}

func (w *BaseWorker) processMessages(ctx context.Context, handler func(*nats.Msg)) error {
	sub, err := w.js.PullSubscribe(w.subject, "", 
		nats.Durable(w.consumer),
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.DeliverAll(),
		nats.Bind(w.stream, w.consumer),
	)
	if err != nil {
		return err
	}
	w.sub = sub

	logger.Infow("Starting worker", "worker", w.name, "stream", w.stream, "consumer", w.consumer)

	for {
		select {
		case <-ctx.Done():
			logger.Infow("Worker stopping", "worker", w.name)
			return ctx.Err()
		default:
			msgs, err := sub.Fetch(10, nats.MaxWait(2*time.Second))
			if err != nil && err != nats.ErrTimeout {
				logger.Errorw("Error fetching messages", "worker", w.name, "error", err)
				continue
			}

			for _, msg := range msgs {
				handler(msg)
				if err := msg.Ack(); err != nil {
					logger.Errorw("Error acknowledging message", "worker", w.name, "error", err)
				}
			}
		}
	}
}