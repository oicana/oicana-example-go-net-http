# Oicana Example Go net/http

Small example Go web service with [net/http](https://pkg.go.dev/net/http) that uses Oicana for PDF templating.

## Getting Started

1. Install the native Oicana library and link it: `export CGO_LDFLAGS="$(go run go.oicana.com/oicana/cmd/oicana-install)"`
2. Start the service: `go run .`
3. Visit http://127.0.0.1:3006/swagger/ for the Swagger documentation

The Swagger documentation is generated from the comments on the handlers with [swag](https://github.com/swaggo/swag). Run `go tool swag init --outputTypes go` after changing them.

## Licensing

The code of this example project is licensed under the [MIT license](LICENSE).

But please be aware, that the Oicana dependency [is licensed under PolyForm Noncommercial License 1.0.0][oicana-license] and, after 30 days, requires a commercial license for use cases that are not covered by the PolyForm license. Visit [the Oicana website][oicana-website] for pricing options.


[oicana-license]: https://github.com/oicana/oicana?tab=readme-ov-file#licensing
[oicana-website]: https://oicana.com
