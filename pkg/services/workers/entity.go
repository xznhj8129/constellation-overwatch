package workers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
	"github.com/nats-io/nats.go"
)

type EntityWorker struct {
	*BaseWorker
}

func NewEntityWorker(nc *nats.Conn, js nats.JetStreamContext) *EntityWorker {
	return &EntityWorker{
		BaseWorker: NewBaseWorker(
			"EntityWorker",
			nc,
			js,
			shared.StreamEntities,
			shared.ConsumerEntityProcessor,
			shared.SubjectEntitiesAll,
		),
	}
}

func (w *EntityWorker) Start(ctx context.Context) error {
	return w.processMessages(ctx, func(msg *nats.Msg) error {
		logger.Infow("Received entity message", "worker", w.Name(), "subject", msg.Subject)

		var data map[string]interface{}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			logger.Errorw("Failed to unmarshal entity message", "worker", w.Name(), "error", err, "data", string(msg.Data))
			return fmt.Errorf("invalid JSON: %w", err)
		}

		prettyJSON, _ := json.MarshalIndent(data, "", "  ")
		logger.Debugw("Entity data", "worker", w.Name(), "json", string(prettyJSON))

		// TODO: Process the entity message here

		return nil
	})
}
