# Changelog

## Unreleased

### Breaking Changes

* `EventFields` now returns a shallow top-level copy. Nested maps/slices are shared references.
* Public API simplified:
  * Removed `AddMap`
  * Removed `Add2`
  * `Add` now accepts variadic key/value pairs: `Add(ctx, "k1", v1, "k2", v2, ...)`

## [0.0.1](https://github.com/happytoolin/happycontext/compare/happycontext-v0.0.1...happycontext-v0.0.1) (2026-02-09)


### Features

* add comprehensive benchmarking for logging adapters and integrations ([5d0f607](https://github.com/happytoolin/happycontext/commit/5d0f6078c137f98fca1a240821712b74b430002a))
* enhance event handling and middleware logging ([0180f08](https://github.com/happytoolin/happycontext/commit/0180f08307d69b4f6ad4a036783b4f09864765f4))
* opensourcing ([c97d378](https://github.com/happytoolin/happycontext/commit/c97d3787dcac19bdb716bdf35fa3020bf0a7775a))


### Miscellaneous Chores

* prepare v0.0.1 release ([9ea1198](https://github.com/happytoolin/happycontext/commit/9ea119821fbb72a88cc75b9affdf8ca87cb01e6b))

## Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, and this project follows Semantic Versioning.
