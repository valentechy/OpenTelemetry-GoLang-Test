hello:
	echo "Hola Friki!!"

run:
	export OTEL_RESOURCE_ATTRIBUTES="service.name=dice,service.version=0.1.0"
	go run main.go

build:
	go build -o bin/main main.go