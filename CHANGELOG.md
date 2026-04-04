# Changelog

## Unreleased

### Breaking Changes

* `EventFields` now returns a shallow top-level copy. Nested maps/slices are shared references.
* Public API simplified:
  * Removed `AddMap`
  * Removed `Add2`
  * `Add` now accepts variadic key/value pairs: `Add(ctx, "k1", v1, "k2", v2, ...)`

## [0.2.3](https://github.com/happytoolin/happycontext/compare/v0.2.2...v0.2.3) (2026-04-04)


### Bug Fixes

* repair root release-please tag matching ([590dcce](https://github.com/happytoolin/happycontext/commit/590dccec72e91c5c444c46c310cc5158c37817fa))
* stop root package-name from shadowing root tags ([68fb797](https://github.com/happytoolin/happycontext/commit/68fb7973f126eb268f3268af22fc4a382b77960f))

## [0.2.2](https://github.com/happytoolin/happycontext/compare/v0.2.1...v0.2.2) (2026-04-04)


### Bug Fixes

* align workspace module requirements ([50f82b3](https://github.com/happytoolin/happycontext/commit/50f82b36d02769e80445c6d1a7919cb086929a4c))

## [0.2.1](https://github.com/happytoolin/happycontext/compare/v0.2.0...v0.2.1) (2026-04-04)


### Miscellaneous Chores

* fix go module release tagging ([8e318cb](https://github.com/happytoolin/happycontext/commit/8e318cbc4d544eec2da3a012b005ea7ebe967533))

## [0.2.0](https://github.com/happytoolin/happycontext/compare/happycontext-v0.1.0...happycontext-v0.2.0) (2026-04-03)


### Features

* document per-request message overrides ([1b78d4f](https://github.com/happytoolin/happycontext/commit/1b78d4f8da42dc5f5e5c4e08aad8a51b3476852a))

## [0.1.0](https://github.com/happytoolin/happycontext/compare/happycontext-v0.0.1...happycontext-v0.1.0) (2026-02-10)


### Features

* Introduce advanced sampling options with per-level rates and custom samplers, and ensure TestSink deep copies event fields. ([537ed99](https://github.com/happytoolin/happycontext/commit/537ed994ee4a437f8d9d6531db1b212ed9e6ca9e))

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
