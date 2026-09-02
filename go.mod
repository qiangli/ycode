module github.com/qiangli/ycode

go 1.26.5

replace mvdan.cc/sh/v3 => ../sh

// Sibling-path replace: works in the dhnt umbrella (../nadir is the
// dhnt/nadir submodule). For standalone clones of ycode, run
// script/bootstrap-siblings.sh — it materialises ../sh and ../nadir
// at the SHAs in .sibling-pins so this replace resolves.
replace github.com/qiangli/nadir => ../nadir

replace github.com/qiangli/bashy => ../bashy

// Bashy's filebrowser replacement does not propagate to embedding modules.
replace github.com/filebrowser/filebrowser/v2 => ../filebrowser

replace github.com/ergochat/readline => ../readline

// Bashy's engine-module replacements are also non-transitive.
replace github.com/qiangli/coreutils/external/otel => ../coreutils/external/otel

replace github.com/ollama/ollama => ../coreutils/external/ollama/src

replace go.podman.io/podman/v6 => ../coreutils/external/podman/src

replace github.com/qiangli/coreutils/pkg/oci => ../coreutils/pkg/oci

replace github.com/jaegertracing/jaeger => github.com/qiangli/jaeger v0.0.0-20260426223533-5aaa7eb1f040

replace github.com/perses/perses => github.com/qiangli/perses v0.0.0-20260426190059-de437951b5e6

require (
	codeberg.org/readeck/go-readability/v2 v2.1.1
	github.com/JohannesKaufmann/html-to-markdown/v2 v2.5.2
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/blevesearch/bleve/v2 v2.5.7
	github.com/bmatcuk/doublestar/v4 v4.10.0
	github.com/bwmarrin/discordgo v0.29.0
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/glamour v1.0.0
	github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834
	github.com/charmbracelet/x/ansi v0.11.6
	github.com/charmbracelet/x/exp/teatest v0.0.0-20260422141420-a6cbdff8a7e2
	github.com/creack/pty/v2 v2.0.1
	github.com/dgraph-io/dgo/v250 v250.0.0
	github.com/dhnt/dhnt v0.2.0-alpha.3.0.20260619230448-ddbed43582c0
	github.com/go-git/go-git/v5 v5.19.1
	github.com/google/go-github/v84 v84.0.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674
	github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
	github.com/nats-io/nats-server/v2 v2.12.6
	github.com/nats-io/nats.go v1.50.0
	github.com/nguyenthenguyen/docx v0.0.0-20230621112118-9c8e795a11db
	github.com/philippgille/chromem-go v0.7.0
	github.com/prometheus/common v0.67.5 // indirect
	github.com/qiangli/aperio v0.0.0-20260506091308-bb748c16502c
	github.com/qiangli/bashy v0.0.0
	github.com/qiangli/bonsai v0.0.0-20260505184649-a3cb69dbf211
	github.com/qiangli/nadir v0.0.0-20260513032315-67009486cf9c
	github.com/spf13/cobra v1.10.2
	github.com/xuri/excelize/v2 v2.10.1
	go.etcd.io/bbolt v1.4.3
	go.opentelemetry.io/contrib/bridges/otelslog v0.18.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0
	go.opentelemetry.io/contrib/instrumentation/runtime v0.68.0
	go.opentelemetry.io/otel v1.44.0
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc v0.19.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.44.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutlog v0.19.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.43.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.43.0
	go.opentelemetry.io/otel/log v0.20.0
	go.opentelemetry.io/otel/metric v1.44.0
	go.opentelemetry.io/otel/sdk v1.44.0
	go.opentelemetry.io/otel/sdk/log v0.20.0
	go.opentelemetry.io/otel/sdk/metric v1.44.0
	go.opentelemetry.io/otel/trace v1.44.0
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/net v0.56.0
	golang.org/x/term v0.44.0
	google.golang.org/grpc v1.81.1
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.50.1
	mvdan.cc/sh/v3 v3.13.1
)

require (
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.20.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.44.0
)

require (
	cyphar.com/go-pathrs v0.2.4 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/GehirnInc/crypt v0.0.0-20230320061759-8cc1b52080c5 // indirect
	github.com/STARRY-S/zip v0.2.3 // indirect
	github.com/VividCortex/ewma v1.2.0 // indirect
	github.com/a2aproject/a2a-go/v2 v2.3.1 // indirect
	github.com/acarl005/stripansi v0.0.0-20180116102854-5a71ef0e047d // indirect
	github.com/aead/serpent v0.0.0-20160714141033-fba169763ea6 // indirect
	github.com/agnivade/levenshtein v1.2.1 // indirect
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/apache/arrow/go/arrow v0.0.0-20211112161151-bc219186db40 // indirect
	github.com/asticode/go-astikit v0.59.0 // indirect
	github.com/asticode/go-astisub v0.40.0 // indirect
	github.com/asticode/go-astits v1.15.0 // indirect
	github.com/aymanbagabas/go-pty v0.2.3 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/benhoyt/goawk v1.31.0 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/bodgit/plumbing v1.3.0 // indirect
	github.com/bodgit/sevenzip v1.6.4 // indirect
	github.com/bodgit/windows v1.0.1 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/bytedance/sonic v1.14.0 // indirect
	github.com/bytedance/sonic/loader v0.3.0 // indirect
	github.com/checkpoint-restore/checkpointctl v1.5.0 // indirect
	github.com/checkpoint-restore/go-criu/v7 v7.2.0 // indirect
	github.com/chewxy/hm v1.0.0 // indirect
	github.com/chromedp/cdproto v0.0.0-20260321001828-e3e3800016bc // indirect
	github.com/chromedp/chromedp v0.15.1 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/chzyer/readline v1.5.1 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/coder/acp-go-sdk v0.13.5 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v1.0.0-rc.4 // indirect
	github.com/containerd/stargz-snapshotter/estargz v0.18.2 // indirect
	github.com/containerd/typeurl/v2 v2.2.3 // indirect
	github.com/containers/common v0.64.2 // indirect
	github.com/containers/gvisor-tap-vsock v0.8.8 // indirect
	github.com/containers/libhvee v0.11.0 // indirect
	github.com/containers/libtrust v0.0.0-20230121012942-c1716e8a8d01 // indirect
	github.com/containers/luksy v0.0.0-20251208191447-ca096313c38f // indirect
	github.com/containers/ocicrypt v1.3.0 // indirect
	github.com/containers/psgo v1.10.0 // indirect
	github.com/containers/winquit v1.1.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/crc-org/vfkit v0.6.3 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/cyberphone/json-canonicalization v0.0.0-20241213102144-19d51d7fe467 // indirect
	github.com/d4l3k/go-bfloat16 v0.0.0-20211005043715-690c3bdd05f1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/digitalocean/go-libvirt v0.0.0-20220804181439-8648fbde413e // indirect
	github.com/digitalocean/go-qemu v0.0.0-20250212194115-ee9b0668d242 // indirect
	github.com/disintegration/imaging v1.6.2 // indirect
	github.com/disiqueira/gotree/v3 v3.0.2 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dlclark/regexp2/v2 v2.1.0 // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/docker-credential-helpers v0.9.6 // indirect
	github.com/docker/go-connections v0.7.0 // indirect
	github.com/docker/go-plugins-helpers v0.0.0-20240701071450-45e2431495c8 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707 // indirect
	github.com/dsoprea/go-exif/v3 v3.0.1 // indirect
	github.com/dsoprea/go-logging v0.0.0-20200710184922-b02d349568dd // indirect
	github.com/dsoprea/go-utility/v2 v2.0.0-20221003172846-a3e1774ef349 // indirect
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/emirpasic/gods/v2 v2.0.0-alpha // indirect
	github.com/ergochat/readline v0.1.3 // indirect
	github.com/filebrowser/filebrowser/v2 v2.63.23 // indirect
	github.com/flynn/go-shlex v0.0.0-20150515145356-3f9db97f8568 // indirect
	github.com/fsouza/go-dockerclient v1.13.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.8 // indirect
	github.com/gin-contrib/cors v1.7.2 // indirect
	github.com/gin-contrib/sse v1.1.0 // indirect
	github.com/gin-gonic/gin v1.11.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.27.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/go-containerregistry v0.21.1 // indirect
	github.com/google/go-intervals v0.0.2 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/gorilla/handlers v1.5.2 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/gorilla/schema v1.4.1 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/itchyny/gojq v0.12.19 // indirect
	github.com/itchyny/timefmt-go v0.1.8 // indirect
	github.com/jellydator/ttlcache/v3 v3.4.0 // indirect
	github.com/jinzhu/copier v0.4.0 // indirect
	github.com/klauspost/pgzip v1.2.6 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20251013123823-9fd1530e3ec3 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/manifoldco/promptui v0.9.0 // indirect
	github.com/maruel/natural v1.3.0 // indirect
	github.com/marusama/semaphore/v2 v2.5.0 // indirect
	github.com/mattn/go-shellwords v1.0.13 // indirect
	github.com/mattn/go-sqlite3 v1.14.42 // indirect
	github.com/mholt/archives v0.1.5 // indirect
	github.com/miekg/pkcs11 v1.1.1 // indirect
	github.com/mikelolasagasti/xz v1.0.1 // indirect
	github.com/minio/minlz v1.1.1 // indirect
	github.com/mistifyio/go-zfs/v4 v4.0.0 // indirect
	github.com/moby/buildkit v0.29.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.2.0 // indirect
	github.com/moby/moby/api v1.54.2 // indirect
	github.com/moby/moby/client v0.4.1 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/capability v0.4.0 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/nlpodyssey/gopickle v0.3.0 // indirect
	github.com/nwaples/rardecode/v2 v2.2.3 // indirect
	github.com/nxadm/tail v1.4.11 // indirect
	github.com/odvcencio/gotreesitter v0.16.0 // indirect
	github.com/ollama/ollama v0.0.0-00010101000000-000000000000 // indirect
	github.com/opencontainers/cgroups v0.0.6 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/opencontainers/runc v1.4.2 // indirect
	github.com/opencontainers/runtime-spec v1.3.0 // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20260316125833-8a4db579f5c8 // indirect
	github.com/opencontainers/selinux v1.13.1 // indirect
	github.com/openshift/imagebuilder v1.2.20 // indirect
	github.com/pdevine/tensor v0.0.0-20240510204454-f88f4562727c // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/pkg/sftp v1.13.10 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/proglottis/gpgme v0.1.6 // indirect
	github.com/qiangli/coreutils/external/otel v0.0.0 // indirect
	github.com/qiangli/coreutils/pkg/oci v0.0.0-00010101000000-000000000000 // indirect
	github.com/qiangli/gfy v0.0.0-20260504062854-764095a2877d // indirect
	github.com/quic-go/qpack v0.5.1 // indirect
	github.com/quic-go/quic-go v0.54.0 // indirect
	github.com/redis/go-redis/v9 v9.20.1 // indirect
	github.com/rjeczalik/notify v0.9.3 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/sabhiram/go-gitignore v0.0.0-20210923224102-525f6e181f06 // indirect
	github.com/seccomp/libseccomp-golang v0.11.1 // indirect
	github.com/secure-systems-lab/go-securesystemslib v0.11.0 // indirect
	github.com/shirou/gopsutil/v4 v4.26.5 // indirect
	github.com/sigstore/fulcio v1.8.5 // indirect
	github.com/sigstore/protobuf-specs v0.5.0 // indirect
	github.com/sigstore/sigstore v1.10.5 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e // indirect
	github.com/smallstep/pkcs7 v0.1.1 // indirect
	github.com/sorairolake/lzip-go v0.3.8 // indirect
	github.com/stangelandcl/ppmd v0.1.1 // indirect
	github.com/stefanberger/go-pkcs11uri v0.0.0-20230803200340-78284954bff6 // indirect
	github.com/sylabs/sif/v2 v2.24.0 // indirect
	github.com/tchap/go-patricia/v2 v2.3.3 // indirect
	github.com/tiktoken-go/tokenizer v0.8.0 // indirect
	github.com/tjfoc/gmsm v1.4.1 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/tklauser/ps v0.0.5-0.20260804061010-39c4acb07b31 // indirect
	github.com/tomasen/realip v0.0.0-20180522021738-f0c99a92ddce // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/u-root/u-root v0.16.0 // indirect
	github.com/ugorji/go/codec v1.3.0 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	github.com/vbatts/tar-split v0.12.2 // indirect
	github.com/vbauerster/mpb/v8 v8.12.0 // indirect
	github.com/vishvananda/netlink v1.3.1 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xtgo/set v1.0.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.podman.io/buildah v1.42.1-0.20260421143840-0acb6b8cca85 // indirect
	go.podman.io/common v0.67.2-0.20260423135811-cbaa5f41e643 // indirect
	go.podman.io/image/v5 v5.39.3-0.20260423135811-cbaa5f41e643 // indirect
	go.podman.io/podman/v6 v6.0.0-20260424181651-a8c36318565d // indirect
	go.podman.io/storage v1.62.1-0.20260423135811-cbaa5f41e643 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/mock v0.5.0 // indirect
	go4.org v0.0.0-20260112195520-a5071408f32f // indirect
	go4.org/unsafe/assume-no-moving-gc v0.0.0-20231121144256-b99613f794b6 // indirect
	golang.org/x/arch v0.20.0 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/tools v0.46.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	gonum.org/v1/gonum v0.17.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gorgonia.org/vecf32 v0.9.0 // indirect
	gorgonia.org/vecf64 v0.9.0 // indirect
	lukechampine.com/blake3 v1.4.1 // indirect
	resty.dev/v3 v3.0.0-rc.2 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
	tags.cncf.io/container-device-interface v1.1.0 // indirect
	tags.cncf.io/container-device-interface/specs-go v1.1.0 // indirect
)

require (
	contrib.go.opencensus.io/exporter/prometheus v0.4.2 // indirect
	dario.cat/mergo v1.0.2 // indirect
	github.com/HdrHistogram/hdrhistogram-go v1.2.0 // indirect
	github.com/JohannesKaufmann/dom v0.3.1 // indirect
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/RoaringBitmap/roaring/v2 v2.16.0 // indirect
	github.com/alecthomas/chroma/v2 v2.23.1 // indirect
	github.com/andybalholm/cascadia v1.3.4 // indirect
	github.com/antithesishq/antithesis-sdk-go v0.6.0-default-no-op // indirect
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/aymanbagabas/go-udiff v0.3.1 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.24.4 // indirect
	github.com/blevesearch/bleve_index_api v1.3.11 // indirect
	github.com/blevesearch/geo v0.2.5 // indirect
	github.com/blevesearch/go-faiss v1.0.30 // indirect
	github.com/blevesearch/go-porterstemmer v1.0.3 // indirect
	github.com/blevesearch/gtreap v0.1.1 // indirect
	github.com/blevesearch/mmap-go v1.2.0 // indirect
	github.com/blevesearch/scorch_segment_api/v2 v2.4.5 // indirect
	github.com/blevesearch/segment v0.9.1 // indirect
	github.com/blevesearch/snowballstem v0.9.0 // indirect
	github.com/blevesearch/upsidedown_store_api v1.0.2 // indirect
	github.com/blevesearch/vellum v1.2.0 // indirect
	github.com/blevesearch/zapx/v11 v11.4.3 // indirect
	github.com/blevesearch/zapx/v12 v12.4.3 // indirect
	github.com/blevesearch/zapx/v13 v13.4.3 // indirect
	github.com/blevesearch/zapx/v14 v14.4.3 // indirect
	github.com/blevesearch/zapx/v15 v15.4.3 // indirect
	github.com/blevesearch/zapx/v16 v16.3.2 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/exp/golden v0.0.0-20241011142426-46044092ad91 // indirect
	github.com/charmbracelet/x/exp/slice v0.0.0-20250327172914-2fdc97757edf // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/chewxy/math32 v1.11.1 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/dgraph-io/badger/v4 v4.9.1 // indirect
	github.com/dgraph-io/ristretto/v2 v2.4.0 // indirect
	github.com/dgraph-io/simdjson-go v0.3.0 // indirect
	github.com/dgryski/go-farm v0.0.0-20240924180020-3414d57e47da // indirect
	github.com/dgryski/go-groupvarint v0.0.0-20230630160417-2bfb7969fb3c // indirect
	github.com/dlclark/regexp2 v1.12.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/elazarl/goproxy v1.8.3 // indirect
	github.com/emirpasic/gods v1.18.1 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/felixge/fgprof v0.9.5 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-git/gcfg v1.5.1-0.20230307220236-3a3c6141e376 // indirect
	github.com/go-git/go-billy/v5 v5.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-shiori/dom v0.0.0-20230515143342-73569d674e1c // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gogs/chardet v0.0.0-20211120154057-b7413eaefb8f // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/golang/geo v0.0.0-20260612074446-f1a45663b0f3 // indirect
	github.com/golang/glog v1.2.5 // indirect
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/codesearch v1.2.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/pprof v0.0.0-20260507013755-92041b743c96 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jbenet/go-context v0.0.0-20150711004518-d14ea06fba99 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kevinburke/ssh_config v1.6.0 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mattn/go-localereader v0.0.2-0.20220822084749-2491eb6c1c75 // indirect
	github.com/mattn/go-runewidth v0.0.24 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/minio/highwayhash v1.0.4-0.20251030100505-070ab1a87a76 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/mschoch/smat v0.2.0 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.1 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pelletier/go-toml/v2 v2.3.1 // indirect
	github.com/pjbgf/sha1cd v0.6.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/profile v1.7.0 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/prometheus/statsd_exporter v0.29.0 // indirect
	github.com/qiangli/coreutils v0.0.0
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/richardlehane/mscfb v1.0.6 // indirect
	github.com/richardlehane/msoleps v1.0.6 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rs/zerolog v1.35.1 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/skeema/knownhosts v1.3.2 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/spf13/viper v1.21.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tiendc/go-deepcopy v1.7.2 // indirect
	github.com/twpayne/go-geom v1.6.1 // indirect
	github.com/viterin/partial v1.1.0 // indirect
	github.com/viterin/vek v0.4.3 // indirect
	github.com/xanzy/ssh-agent v0.3.3 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	github.com/yalue/onnxruntime_go v1.30.1 // indirect
	github.com/yuin/goldmark v1.8.2 // indirect
	github.com/yuin/goldmark-emoji v1.0.6 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.43.0
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/image v0.42.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260618152121-87f3d3e198d3 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260618152121-87f3d3e198d3 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
	gopkg.in/warnings.v0 v0.1.2 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace github.com/qiangli/coreutils => ../coreutils

// coreutils' own goawk replace does not propagate across modules, and
// upstream goawk v1.31.0 has no `regex` package — cmds/awk needs the fork.
replace github.com/benhoyt/goawk => ../coreutils/third_party/goawk

exclude (
	google.golang.org/genproto v0.0.0-20200804131852-c06518451d9c
	google.golang.org/genproto v0.0.0-20200825200019-8632dd797987
)

replace google.golang.org/genproto => google.golang.org/genproto v0.0.0-20260622175928-b703f567277d
