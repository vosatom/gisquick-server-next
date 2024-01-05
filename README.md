# Gisquick application server (backend)

```
docker build -t gisquick/server-dev -f ./docker/Dockerfile.dev .
```

```
docker build -t gisquick/server -f ./docker/Dockerfile-alpine .
```


## Run tests

1. Start docker compose

```
docker compose -f docker-compose.test.yml up
```
2. Use vscode's Go extension to run tests. 