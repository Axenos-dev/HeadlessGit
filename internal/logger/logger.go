package logger

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New() (*zap.Logger, error) {
	zapCfg := zap.NewDevelopmentConfig()
	zapCfg.Encoding = "console"
	zapCfg.DisableCaller = true
	zapCfg.DisableStacktrace = true

	zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapCfg.EncoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("15:04:05.000"))
	}
	zapCfg.EncoderConfig.EncodeDuration = zapcore.StringDurationEncoder
	zapCfg.EncoderConfig.ConsoleSeparator = "  "
	return zapCfg.Build()
}
