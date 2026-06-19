# ward-kdl docker

Exec-dialect CLI. Every verb runs `docker` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl docker ps - list containers; -a for stopped, --format to project fields

`docker ps`

Flags: unrestricted passthrough.

## ward-kdl docker logs - container logs; -f to follow (omit for a snapshot), --tail N

`docker logs`

Flags: unrestricted passthrough.

## ward-kdl docker top - running processes in a container

`docker top`

Flags: unrestricted passthrough.

## ward-kdl docker stats - live resource usage; pass --no-stream for a one-shot snapshot

`docker stats`

Flags: unrestricted passthrough.

## ward-kdl docker inspect - low-level JSON for a container/image/volume/network

`docker inspect`

Flags: unrestricted passthrough.

## ward-kdl docker port - published port mappings for a container

`docker port`

Flags: unrestricted passthrough.

## ward-kdl docker diff - filesystem changes against a container's image

`docker diff`

Flags: unrestricted passthrough.

## ward-kdl docker images - list images; --format to project fields

`docker images`

Flags: unrestricted passthrough.

## ward-kdl docker history - layer history of an image

`docker history`

Flags: unrestricted passthrough.

## ward-kdl docker version - client + daemon version

`docker version`

Flags: unrestricted passthrough.

## ward-kdl docker info - daemon-wide info and counts

`docker info`

Flags: unrestricted passthrough.

## ward-kdl docker events - daemon event stream; --since/--until to bound and exit

`docker events`

Flags: unrestricted passthrough.

## ward-kdl docker volume ls - list volumes

`docker volume ls`

Flags: unrestricted passthrough.

## ward-kdl docker volume inspect - low-level JSON for a volume

`docker volume inspect`

Flags: unrestricted passthrough.

## ward-kdl docker network ls - list networks

`docker network ls`

Flags: unrestricted passthrough.

## ward-kdl docker network inspect - low-level JSON for a network

`docker network inspect`

Flags: unrestricted passthrough.

## ward-kdl docker system df - disk usage by images, containers, volumes, cache

`docker system df`

Flags: unrestricted passthrough.
