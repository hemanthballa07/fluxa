package fraudeval

import (
	"context"
	"errors"
	"time"

	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/fraud"
	fraudv1 "github.com/fluxa/fluxa/internal/grpc/fraud/v1"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/ports"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ServiceName = "fraud-grpc"

// Server implements the FraudEval gRPC service.
type Server struct {
	fraudv1.UnimplementedFraudEvalServer
	Engine  *fraud.Engine
	DB      *db.Client
	Metrics ports.Metrics
	Logger  *logging.Logger
	Version string
	// Scorer is the optional ML scorer; nil => rules-only (fail-open). Set by main after NewServer.
	Scorer fraud.Scorer
}

func NewServer(engine *fraud.Engine, dbClient *db.Client, metrics ports.Metrics, logger *logging.Logger, version string) *Server {
	return &Server{
		Engine:  engine,
		DB:      dbClient,
		Metrics: metrics,
		Logger:  logger,
		Version: version,
	}
}

func (s *Server) EvaluateTransaction(ctx context.Context, req *fraudv1.EvaluateRequest) (*fraudv1.EvaluateResponse, error) {
	start := time.Now()

	if req.GetEventId() == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}
	if req.GetTransactionTime() == nil {
		return nil, status.Error(codes.InvalidArgument, "transaction_time is required")
	}

	event := protoToEvent(req)
	if err := event.Validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, status.Error(codes.DeadlineExceeded, err.Error())
		}
		return nil, status.Error(codes.Canceled, err.Error())
	}

	if err := s.DB.InsertEvent(&event, req.GetEventId(), domain.PayloadModeInline, nil); err != nil {
		s.Logger.Error("InsertEvent failed", err, map[string]interface{}{"event_id": req.GetEventId()})
		return nil, status.Error(codes.Unavailable, "failed to persist event")
	}

	flags, mlScore, modelVersion, err := s.Engine.EvaluateWithScorer(ctx, &event, s.DB, s.Scorer)
	if err != nil {
		s.Logger.Error("Engine.EvaluateWithScorer failed", err, map[string]interface{}{"event_id": req.GetEventId()})
		return nil, status.Error(codes.Internal, "fraud evaluation failed")
	}

	for _, flag := range flags {
		flag.MlScore = mlScore
		if dbErr := s.DB.InsertFraudFlag(&flag); dbErr != nil {
			s.Logger.Error("InsertFraudFlag failed", dbErr, map[string]interface{}{
				"event_id":  flag.EventID,
				"rule_name": flag.RuleName,
			})
			continue
		}
		s.Metrics.IncCounter("fraud_flags_grpc_total", "rule", flag.RuleName)
	}

	latency := time.Since(start)
	latencyMs := float64(latency.Microseconds()) / 1000.0
	s.Metrics.ObserveHistogram("fraud_eval_latency_seconds", latency.Seconds(), "service", ServiceName)

	evaluatedBy := s.Version
	if modelVersion != "" && modelVersion != "unavailable" {
		evaluatedBy = s.Version + "+ml-" + modelVersion
	}

	return &fraudv1.EvaluateResponse{
		Decision:    decisionFromFlags(flags),
		Flags:       toProtoFlags(flags),
		LatencyMs:   latencyMs,
		EvaluatedBy: evaluatedBy,
		MlScore:     mlScore,
	}, nil
}

func protoToEvent(req *fraudv1.EvaluateRequest) domain.Event {
	md := req.GetMetadata()
	var metadata map[string]interface{}
	if len(md) > 0 {
		metadata = make(map[string]interface{}, len(md))
		for k, v := range md {
			metadata[k] = v
		}
	}
	return domain.Event{
		EventID:   req.GetEventId(),
		UserID:    req.GetUserId(),
		Amount:    req.GetAmount(),
		Currency:  req.GetCurrency(),
		Merchant:  req.GetMerchant(),
		Timestamp: req.GetTransactionTime().AsTime(),
		Metadata:  metadata,
	}
}

func toProtoFlags(flags []domain.FraudFlag) []*fraudv1.FraudFlag {
	if len(flags) == 0 {
		return nil
	}
	out := make([]*fraudv1.FraudFlag, len(flags))
	for i, f := range flags {
		out[i] = &fraudv1.FraudFlag{
			RuleName:  f.RuleName,
			RuleValue: f.RuleValue,
		}
	}
	return out
}

func decisionFromFlags(flags []domain.FraudFlag) fraudv1.Decision {
	if len(flags) == 0 {
		return fraudv1.Decision_DECISION_ALLOW
	}
	return fraudv1.Decision_DECISION_FLAG
}

// LoggingInterceptor emits one JSON log line per RPC with code + latency.
func LoggingInterceptor(logger *logging.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		fields := map[string]interface{}{
			"method":     info.FullMethod,
			"code":       code.String(),
			"latency_ms": float64(time.Since(start).Microseconds()) / 1000.0,
		}
		if er, ok := req.(*fraudv1.EvaluateRequest); ok {
			fields["event_id"] = er.GetEventId()
		}
		if err != nil {
			logger.Error("rpc failed", err, fields)
		} else {
			logger.Info("rpc ok", fields)
		}
		return resp, err
	}
}
