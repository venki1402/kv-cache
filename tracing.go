package main

import (
	"context"
	"time"

	"github.com/valyala/fasthttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// tracingEnabled gates every tracing code path so the disabled case costs a
// single branch per request.
var (
	tracingEnabled bool
	tracer         trace.Tracer = noop.NewTracerProvider().Tracer("kv-cache")
	propagator                  = propagation.TraceContext{}
)

const spanCtxKey = "otel-span-ctx"

// Endpoint and TLS mode come from the standard OTEL_EXPORTER_OTLP_* env vars.
func setupTracing(ctx context.Context, sampleRate float64) (func(context.Context) error, error) {
	exp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	res := resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceName("kv-cache"),
		semconv.ServiceVersion(version),
		attribute.String("vcs.revision", commit),
	)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRate))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagator)
	tracer = tp.Tracer("kv-cache")
	tracingEnabled = true
	return tp.Shutdown, nil
}

type headerCarrier struct{ h *fasthttp.RequestHeader }

func (c headerCarrier) Get(key string) string { return string(c.h.Peek(key)) }
func (c headerCarrier) Set(key, value string) { c.h.Set(key, value) }
func (c headerCarrier) Keys() []string {
	var keys []string
	for k := range c.h.All() {
		keys = append(keys, string(k))
	}
	return keys
}

func traced(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		parent := propagator.Extract(context.Background(), headerCarrier{&ctx.Request.Header})
		route := routeNames[routeOf(ctx.Path())]
		method := string(ctx.Method())
		spanCtx, span := tracer.Start(parent, method+" "+route,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(method),
				semconv.HTTPRoute(route),
			))
		ctx.SetUserValue(spanCtxKey, spanCtx)

		next(ctx)

		code := ctx.Response.StatusCode()
		span.SetAttributes(semconv.HTTPResponseStatusCode(code))
		if code >= 500 {
			span.SetStatus(codes.Error, "server error")
		}
		span.End()
	}
}

func requestContext(ctx *fasthttp.RequestCtx) context.Context {
	if c, ok := ctx.UserValue(spanCtxKey).(context.Context); ok {
		return c
	}
	return context.Background()
}

func traceIDFromCtx(ctx *fasthttp.RequestCtx) string {
	if !tracingEnabled {
		return ""
	}
	sc := trace.SpanContextFromContext(requestContext(ctx))
	if !sc.HasTraceID() {
		return ""
	}
	return sc.TraceID().String()
}
