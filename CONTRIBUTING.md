# Contributing

Thanks for helping. Bug reports, mapping corrections, and implementation reports are all welcome.

## Reporting issues

- **Wrong or missing mapping:** open an issue with the span attributes you see and the OASA record you expected. Link the OpenTelemetry semantic-convention page if an attribute name has changed upstream.
- **Spec questions:** field definitions belong to the [OASA specification](https://github.com/onaro-io/agent-spend-attribution). A change to what a field means is a spec pull request first; this exporter follows it.
- **Security issues:** don't open a public issue. See [SECURITY.md](SECURITY.md).

## Pull requests

1. Fork the repository and branch from `main`.
2. Run `make lint test` before pushing. Run `make integration` if you changed the factory, config, or sinks; it builds a collector with `ocb`.
3. Add or update tests:
   - every mapping row has a unit test in `translate_test.go`;
   - a change to output for the golden fixtures needs the matching example updated in the spec repository first, then `make sync-spec`.
4. Mapping changes go in `mapping.yaml`, not in Go code.
5. Keep commits focused and describe the user-visible change in `CHANGELOG.md` under "Unreleased".

By contributing, you agree that your contributions are licensed under the Apache License 2.0.

## Code of conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).
