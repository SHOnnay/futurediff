package dockerrun

import "context"

type GatewayExecutor struct {
	Runtime Runtime
	Image   string
	Env     map[string]string
}

func (e GatewayExecutor) Run(ctx context.Context, worktreePath string, command []string) ([]byte, error) {
	return e.Runtime.Run(ctx, Config{
		Image:       e.Image,
		Workdir:     worktreePath,
		MountSource: worktreePath,
		Command:     command,
		Env:         e.Env,
	})
}
