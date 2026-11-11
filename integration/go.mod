module github.com/habitat-network/habitat/integration

go 1.26.3

replace github.com/docker/docker/api => github.com/moby/moby/api v1.55.0

exclude google.golang.org/genproto v0.0.0-20190626174449-989357319d63

require github.com/stretchr/testify v1.12.1

require go.yaml.in/yaml/v3 v3.0.5 // indirect
