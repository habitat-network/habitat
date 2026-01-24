module github.com/eagraf/habitat-new/cmd/privi

go 1.25.4

replace github.com/eagraf/habitat-new => ../..

require (
	github.com/bluesky-social/indigo v0.0.0-20251203031309-bdbb48c13b04
	github.com/bluesky-social/jetstream v0.0.0-20251009222037-7d7efa58d7f1
	github.com/eagraf/habitat-new v0.0.0-00010101000000-000000000000
	github.com/go-jose/go-jose/v3 v3.0.4
	github.com/gorilla/sessions v1.4.0
	github.com/rs/zerolog v1.34.0
	github.com/urfave/cli-altsrc/v3 v3.1.0
	github.com/urfave/cli/v3 v3.6.1
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.64.0
	go.opentelemetry.io/otel v1.39.0
	go.opentelemetry.io/otel/log v0.14.0
	go.opentelemetry.io/otel/metric v1.39.0
	golang.org/x/sync v0.18.0
	gorm.io/driver/postgres v1.5.7
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.1
)

require (
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cristalhq/jwt/v4 v4.0.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dgraph-io/ristretto v1.0.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/earthboundkid/versioninfo/v2 v2.24.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gobuffalo/pop/v6 v6.1.1 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/mock v1.7.0-rc.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/schema v1.4.1 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.3 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.7 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/ipfs/go-block-format v0.2.0 // indirect
	github.com/ipfs/go-cid v0.6.0 // indirect
	github.com/ipfs/go-ipfs-util v0.0.3 // indirect
	github.com/ipfs/go-ipld-cbor v0.1.0 // indirect
	github.com/ipfs/go-ipld-format v0.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/pgx/v5 v5.5.0 // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-sqlite3 v1.14.32 // indirect
	github.com/mattn/goveralls v0.0.12 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/mr-tron/base58 v1.2.0 // indirect
	github.com/multiformats/go-base32 v0.1.0 // indirect
	github.com/multiformats/go-base36 v0.2.0 // indirect
	github.com/multiformats/go-multibase v0.2.0 // indirect
	github.com/multiformats/go-multihash v0.2.3 // indirect
	github.com/multiformats/go-varint v0.1.0 // indirect
	github.com/openzipkin/zipkin-go v0.4.2 // indirect
	github.com/ory/fosite v0.49.0 // indirect
	github.com/ory/go-acc v0.2.9-0.20230103102148-6b1c9a70dbbe // indirect
	github.com/ory/go-convenience v0.1.0 // indirect
	github.com/ory/x v0.0.665 // indirect
	github.com/pelletier/go-toml/v2 v2.1.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/polydawn/refmt v0.89.1-0.20221221234430-40501e09de1f // indirect
	github.com/prometheus/client_golang v1.19.1 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.54.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/sagikazarmark/locafero v0.4.0 // indirect
	github.com/sagikazarmark/slog-shim v0.1.0 // indirect
	github.com/seatgeek/logrus-gelf-formatter v0.0.0-20210414080842-5b05eb8ff761 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spaolacci/murmur3 v1.1.0 // indirect
	github.com/spf13/afero v1.11.0 // indirect
	github.com/spf13/cast v1.6.0 // indirect
	github.com/spf13/cobra v1.8.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/spf13/viper v1.18.2 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/whyrusleeping/cbor-gen v0.2.1-0.20241030202151-b7a6831be65e // indirect
	github.com/x448/float16 v0.8.4 // indirect
	gitlab.com/yawning/secp256k1-voi v0.0.0-20230925100816-f2616030848b // indirect
	gitlab.com/yawning/tuplehash v0.0.0-20230713102510-df83abbf9a02 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.46.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/runtime v0.64.0 // indirect
	go.opentelemetry.io/contrib/propagators/b3 v1.21.0 // indirect
	go.opentelemetry.io/contrib/propagators/jaeger v1.21.1 // indirect
	go.opentelemetry.io/contrib/samplers/jaegerremote v0.15.1 // indirect
	go.opentelemetry.io/otel/exporters/jaeger v1.17.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.15.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.39.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.39.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.39.0 // indirect
	go.opentelemetry.io/otel/exporters/zipkin v1.21.0 // indirect
	go.opentelemetry.io/otel/sdk v1.39.0 // indirect
	go.opentelemetry.io/otel/sdk/log v0.15.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.39.0 // indirect
	go.opentelemetry.io/otel/trace v1.39.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/exp v0.0.0-20240604190554-fc45aab8b7f8 // indirect
	golang.org/x/mod v0.29.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/oauth2 v0.33.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.38.0 // indirect
	golang.org/x/xerrors v0.0.0-20231012003039-104605ab7028 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/grpc v1.77.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
)
