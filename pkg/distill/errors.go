package distill

import "errors"

// ErrNilWriter is returned by Distill when Options.Writer is nil.
// Distill needs a destination for the chosen Sink's output; a nil
// Writer is a setup error, not a runtime condition.
var ErrNilWriter = errors.New("distill: Options.Writer is nil")

// ErrUnknownOutput is returned by Distill when Options.Output is not
// one of the documented OutputFormat constants. Library callers
// passing OutputText / OutputJSON / OutputJSONStreaming /
// OutputMarkdown never see this; it exists for cases where the
// caller passes a string from a config file or CLI flag without
// validating first.
var ErrUnknownOutput = errors.New("distill: unknown Output value")

// ErrUnknownTokenizer is returned by Distill when Options.Tokenizer
// is non-empty and not one of "heuristic" / "tiktoken". The error
// surfaces before any pipeline goroutine starts so the caller sees
// a deterministic setup failure rather than a mid-stream surprise.
var ErrUnknownTokenizer = errors.New("distill: unknown Tokenizer value")

// ErrUnknownFormat is returned by Distill when Options.Format names
// a format that is not registered. The error wraps the format name
// so callers can render it without re-parsing.
var ErrUnknownFormat = errors.New("distill: unknown Format value")

// ErrUnknownStripEnvelope is returned by Distill when
// Options.StripEnvelope names an envelope stripper that is not
// registered. "auto" and "none" are always valid; specific names
// must match a registered envelope.Stripper.
var ErrUnknownStripEnvelope = errors.New("distill: unknown StripEnvelope value")
