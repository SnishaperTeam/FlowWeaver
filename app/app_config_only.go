package app

import (
	"log"

	"flowweaver/common"
)

// StartupConfigOnly prepares logging and loads the settings and rules without
// starting the core process, the UI or any monitor. It deliberately opens no
// log file and keeps loader output off stdout, so a one-shot CLI command stays
// quiet and does not litter the log directory.
func (a *App) StartupConfigOnly() error {
	if a.logBuffer == nil {
		a.logBuffer = common.NewRingLogWriter(500)
	}
	log.SetOutput(&gatedLogWriter{app: a})
	a.resolveLogDirPath()
	return a.ruleManager.LoadConfig()
}
