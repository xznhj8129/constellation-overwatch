package workers

import (
	"context"
	"encoding/json"

	"constellation-overwatch/pkg/services/logger"
	"constellation-overwatch/pkg/shared"
	"github.com/nats-io/nats.go"
)

type CommandWorker struct {
	*BaseWorker
}

func NewCommandWorker(nc *nats.Conn, js nats.JetStreamContext) *CommandWorker {
	return &CommandWorker{
		BaseWorker: NewBaseWorker(
			"CommandWorker",
			nc,
			js,
			shared.StreamCommands,
			shared.ConsumerCommandProcessor,
			shared.SubjectCommandsAll,
		),
	}
}

func (w *CommandWorker) Start(ctx context.Context) error {
	return w.processMessages(ctx, func(msg *nats.Msg) {
		logger.Infow("Received command message", "worker", w.Name(), "subject", msg.Subject)

		var data map[string]interface{}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			logger.Debugw("Raw message data", "worker", w.Name(), "data", string(msg.Data))
		} else {
			prettyJSON, _ := json.MarshalIndent(data, "", "  ")
			logger.Debugw("Command data", "worker", w.Name(), "json", string(prettyJSON))
		}
	})
}
