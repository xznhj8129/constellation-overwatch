package workers

import (
	"context"
	"time"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"

	"github.com/nats-io/nats.go"
)

type Worker interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
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

func (w *BaseWorker) HealthCheck() error {
	if w.nc != nil && w.nc.IsConnected() {
		return nil
	}
	return nats.ErrConnectionClosed
}

func (w *BaseWorker) Stop(ctx context.Context) error {
	if w.sub != nil {
		// For pull subscriptions, unsubscribe instead of drain
		// Drain() is for push subscriptions and doesn't work properly with pull consumers
		return w.sub.Unsubscribe()
	}
	return nil
}

func (w *BaseWorker) processMessages(ctx context.Context, handler func(*nats.Msg) error) error {
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
			// Check if subscription is still valid before attempting fetch
			if w.sub != nil && !w.sub.IsValid() {
				logger.Infow("Subscription invalid, worker exiting gracefully", "worker", w.name)
				return nil
			}

			msgs, err := sub.Fetch(10, nats.MaxWait(2*time.Second))
			if err != nil {
				// Timeout is expected and normal - just continue
				if err == nats.ErrTimeout {
					continue
				}
				// These errors indicate shutdown or connection closure - exit gracefully
				if err == nats.ErrBadSubscription || err == nats.ErrConnectionClosed {
					logger.Infow("Subscription closed, worker exiting gracefully", "worker", w.name, "error", err)
					return nil
				}
				// For other errors, log and continue
				logger.Errorw("Error fetching messages", "worker", w.name, "error", err)
				continue
			}

			for _, msg := range msgs {
				if err := handler(msg); err != nil {
					// Handler failed - use negative acknowledgement to trigger redelivery
					if nakErr := msg.Nak(); nakErr != nil {
						logger.Errorw("Error sending NAK", "worker", w.name, "error", nakErr)
					}
					logger.Errorw("Handler failed, message NAK'd for redelivery",
						"worker", w.name,
						"subject", msg.Subject,
						"error", err)
				} else {
					// Handler succeeded - acknowledge the message
					if ackErr := msg.Ack(); ackErr != nil {
						logger.Errorw("Error acknowledging message", "worker", w.name, "error", ackErr)
					}
				}
			}
		}
	}
}
