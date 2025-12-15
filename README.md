# Go Playground

Personal testing ground for Go development and experimentation.

# Notes

Minimalist Q&A style. Sections use `##` headings and follow the format:
`<Topic>: <Subtopic>: <Question>`. Sections are alphabetically ordered by
`<Topic>: <Subtopic>`. A table of contents is provided below for navigation.

## Table of Contents

- [BSON: CustomDecoder: How do I register a custom decoder for
  time.Time?](#bson-customdecoder-how-do-i-register-a-custom-decoder-for-timetime)
- [CLI: Grep: How to grep filenames with
  extensions?](#cli-grep-how-to-grep-filenames-with-extensions)
- [Client: EventMonitor: Can you mutate a monitor after it's been
  set?](#client-eventmonitor-can-you-mutate-a-monitor-after-its-been-set)
- [Client: Topology: How do you get a topology description from
  mongo.Client?](#client-topology-how-do-you-get-a-topology-description-from-mongoclient)
- [Errors: ServerError: Can you use errors.As with CommandFailedEvent
  errors?](#errors-servererror-can-you-use-errorsas-with-commandfailedevent-errors)
- [SearchIndex: DefaultBehavior: What happens when name and type are not
  provided?](#searchindex-defaultbehavior-what-happens-when-name-and-type-are-not-provided)
- [Sessions: SnapshotTime: Can clients read at a time outside a session's
  lifecycle?](#sessions-snapshottime-can-clients-read-at-a-time-outside-a-sessions-lifecycle)
- [Sessions: SnapshotTime: How does the server know to return the same
  atClusterTime?](#sessions-snapshottime-how-does-the-server-know-to-return-the-same-atclustertime)
- [Sessions: SnapshotTime: Why does the spec specify "first"
  operation?](#sessions-snapshottime-why-does-the-spec-specify-first-operation)
- [Sessions: SnapshotTime: Why is this session-specific if it's just a
  timestamp?](#sessions-snapshottime-why-is-this-session-specific-if-its-just-a-timestamp)
- [UnifiedSpec: AutoEncryptOpts: How do we decode autoEncryptOpts
  idiomatically?](#unifiedspec-autoencryptopts-how-do-we-decode-autoencryptopts-idiomatically)

## BSON: CustomDecoder: How do I register a custom decoder for time.Time?

[Test: mgd_bson_test.go:45](mgd_bson_test.go#L45)

Create a fresh registry, capture the default decoder, then register a wrapper
that handles your custom type while preserving default behavior:

```go reg := bson.NewRegistry() baseDec, _ :=
reg.LookupDecoder(reflect.TypeOf(time.Time{}))
reg.RegisterTypeDecoder(reflect.TypeOf(time.Time{}), customWrapper(baseDec))
```

## CLI: Grep: How to grep filenames with extensions?

```bash grep -R -l \ --include='*.yml' --include='*.yaml' \
--exclude-dir='testdata' \ -e . .
```

## Client: EventMonitor: Can you mutate a monitor after it's been set?

[Test: mgd_client_test.go:13](mgd_client_test.go#L13)

Yes, using an indirection pattern. Set a function pointer in the monitor that
delegates to a mutable function variable, allowing you to change behavior after
the monitor is attached to the client.

## Client: Topology: How do you get a topology description from mongo.Client?

[Test: mgd_topology_test.go:15](mgd_topology_test.go#L15)

You can't get it directly. Provide a server monitor via `SetServerMonitor()`
that captures topology descriptions in events. The monitor stores the latest
description for access.

## Errors: ServerError: Can you use errors.As with CommandFailedEvent errors?

[Test: mgd_errors_test.go:17](mgd_errors_test.go#L17)

Yes. Errors from `CommandFailedEvent` can be unwrapped using
`errors.As(&serverErr)` to extract `mongo.ServerError` and access error codes.

## SearchIndex: DefaultBehavior: What happens when name and type are not
provided?

[Test: mgd_search_index_test.go:17](mgd_search_index_test.go#L17)

The server generates default values: name becomes `"default"` and type becomes
`"search"`.

## Sessions: SnapshotTime: Can clients read at a time outside a session's
lifecycle?

Yes. The timestamp is just a point in the database's oplog history. The session
is a temporary resource handle saying "I'm currently reading at this time."
Multiple sessions can read at the same timestamp, and you can reuse a timestamp
from an ended session (if the server hasn't compacted that history yet).

## Sessions: SnapshotTime: How does the server know to return the same
atClusterTime?

It doesn't remember. The driver stores the timestamp and sends it in the
readConcern on every operation:
- First op: Driver sends `readConcern: {level: "snapshot"}`, server picks T and
  returns `atClusterTime: T`
- Later ops: Driver sends `readConcern: {level: "snapshot", atClusterTime: T}`,
  server echoes T back

## Sessions: SnapshotTime: Why does the spec specify "first" operation?

The first operation is when the server picks and returns the snapshot timestamp
if none was provided. After that, the driver sends that timestamp back in every
subsequent operation's readConcern. The server just echoes it back.

Specifying "first" makes drivers defensive: capture once when established, then
lock it in. Don't trust that every response will have the same value.

## Sessions: SnapshotTime: Why is this session-specific if it's just a
timestamp?

The server needs the session for resource management. It must maintain MVCC
state at that timestamp and know when to clean up old versions. The session ID
is the cleanup handle—without it, the server wouldn't know when clients are done
reading historical data.

## UnifiedSpec: AutoEncryptOpts: How do we decode autoEncryptOpts idiomatically?

[Test: mgd_auto_encrypt_opts_test.go:15](mgd_auto_encrypt_opts_test.go#L15)

Implement custom `UnmarshalBSON` on the struct to handle placeholder
substitution and environment variable fallback. Use type aliases to avoid
recursion, then apply transformation logic before assigning to the receiver.

