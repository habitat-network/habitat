package telemetry

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Span attribute keys shared by habitat's stores, so a trace can be found by
// the record, space or repo it touched whichever component recorded it. They
// are high-cardinality, so they belong on spans and logs, never on metrics.
const (
	// SpaceURIKey is a network.habitat.space URI.
	SpaceURIKey = attribute.Key("habitat.space.uri")
	// RepoKey is the DID of a repo within a space: the record owner.
	RepoKey = attribute.Key("habitat.repo")
	// CollectionKey is a record's collection NSID.
	CollectionKey = attribute.Key("habitat.record.collection")
	// RkeyKey is a record's key.
	RkeyKey = attribute.Key("habitat.record.rkey")
	// RecordURIKey is a record's full space record URI.
	RecordURIKey = attribute.Key("habitat.record.uri")
	// RecordURIsKey lists the records an operation on several touched.
	RecordURIsKey = attribute.Key("habitat.record.uris")
	// RevKey is a repo or record revision (TID).
	RevKey = attribute.Key("habitat.rev")
	// OrgKey is the DID of the org a request is scoped to.
	OrgKey = attribute.Key("habitat.org")
	// AuthMethodKey names the method that authenticated a request.
	AuthMethodKey = attribute.Key("habitat.auth.method")
)

// EndUser identifies the authenticated caller of a request.
func EndUser(did syntax.DID) attribute.KeyValue {
	return semconv.EnduserID(did.String())
}

// Space is the attribute of a space URI.
func Space(uri habitat_syntax.SpaceURI) attribute.KeyValue {
	return SpaceURIKey.String(uri.String())
}

// Repo is the attribute of a repo DID.
func Repo(did syntax.DID) attribute.KeyValue {
	return RepoKey.String(did.String())
}

// Collection is the attribute of a record collection.
func Collection(nsid syntax.NSID) attribute.KeyValue {
	return CollectionKey.String(nsid.String())
}

// Rkey is the attribute of a record key.
func Rkey(rkey syntax.RecordKey) attribute.KeyValue {
	return RkeyKey.String(rkey.String())
}

// Rev is the attribute of a revision.
func Rev(rev string) attribute.KeyValue {
	return RevKey.String(rev)
}

// Record returns the attributes of a record: its URI, and the space, repo,
// collection and rkey it is made of, so each is searchable on its own.
func Record(uri habitat_syntax.SpaceRecordURI) []attribute.KeyValue {
	return []attribute.KeyValue{
		RecordURIKey.String(uri.String()),
		Space(uri.SpaceURI()),
		Repo(uri.Repo()),
		Collection(uri.Collection()),
		Rkey(uri.Rkey()),
	}
}

// RecordURIs is the attribute listing the records an operation touched.
func RecordURIs(uris []habitat_syntax.SpaceRecordURI) attribute.KeyValue {
	out := make([]string, len(uris))
	for i, uri := range uris {
		out[i] = uri.String()
	}
	return RecordURIsKey.StringSlice(out)
}

// End ends span, first recording err on it as an exception and an error
// status when it is set. Call it deferred with a named error result:
//
//	ctx, span := tracer.Start(ctx, "Op")
//	defer func() { telemetry.End(span, err) }()
func End(span trace.Span, err error) {
	if err != nil {
		RecordError(span, err)
	}
	span.End()
}

// RecordError marks span as failed with err.
func RecordError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
