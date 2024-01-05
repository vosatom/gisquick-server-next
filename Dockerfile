# Prev version
# FROM alpine:3.13 as dbhash-build
# RUN apk add build-base tcl
# RUN mkdir -p /src && wget -O - https://www.sqlite.org/src/tarball/sqlite.tar.gz?r=version-3.38.2 | tar zxf - -C /src
# WORKDIR /src/sqlite
# RUN ./configure && make dbhash

FROM alpine:3.13 as dbhash-build
RUN apk add build-base

RUN wget -qO- https://www.sqlite.org/2022/sqlite-amalgamation-3390200.zip | unzip -j -d /src -
RUN wget -O /src/dbhash.c https://raw.githubusercontent.com/sqlite/sqlite/master/tool/dbhash.c

WORKDIR /src/
# RUN gcc -O3 sqlite3.c dbhash.c -lpthread -ldl -o /usr/local/bin/dbhash
RUN gcc -O3 -DSQLITE_OMIT_LOAD_EXTENSION dbhash.c sqlite3.c -lpthread -lm -o /usr/local/bin/dbhash
# RUN gcc -O3 -DSQLITE_THREADSAFE=0 -DSQLITE_OMIT_LOAD_EXTENSION dbhash.c sqlite3.c -lm -o /usr/local/bin/dbhash


FROM golang:1.18-alpine as build
RUN apk add --no-cache git
WORKDIR /go/src/app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /go/bin/gisquick cmd/main.go


FROM alpine:latest

ENV USERNAME=app
ENV GROUP=app
ENV UID=1000
ENV GID=1000

# USER nonroot:nonroot
# COPY --from=build --chown=nonroot:nonroot /go/bin/app /app
RUN addgroup -g "$GID" -S "$GROUP" && adduser -S -u "$UID" -D -G "$GROUP" "$USERNAME"

COPY --from=dbhash-build /usr/local/bin/dbhash /usr/local/bin/

WORKDIR /app
COPY --from=build /go/src/app/templates ./templates
COPY --from=build /go/src/app/migrations ./migrations
COPY --from=build /go/bin/gisquick /usr/local/bin/

USER ${USERNAME}
EXPOSE 3000

CMD ["gisquick", "serve"]
