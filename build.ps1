go build -trimpath -ldflags "-X main.version=$(git describe --tags --always)" -o ./builds ./cmd/nado
go build -trimpath -ldflags "-X main.version=$(git describe --tags --always)" -o ./builds ./cmd/nado-admin
go build -trimpath -ldflags "-X main.version=$(git describe --tags --always)" -o ./builds ./cmd/nado-jobs
