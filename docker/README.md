# Developers
[developer docs](docs/developer.md)

[build docs](../BUILD.md)

# Build both code and image using Dockerfile
Most Dockerfiles both build the code as well as the image.

# Choose the desired build flavour:
- Dockerfile -> builds more secure version of the image (from scratch instead of basing on alpine)
- DebugDockerfile -> builds a debug version of the image, based on alpine and uses "github.com/derekparker/delve/cmd/dlv". Port :7777
- NotFromScrachDockerfile -> alpine based image (notFromScratch like in Dockerfile )
- NoBuildDockerfile -> alpine based image, without the build, it expects binary is build outside. This is the older version of the Dockerfile file.
- NoBuildDebugDockerfile -> alpine based image, without the build, it expects binary is build outside. This is the older version of the DebugDocker file.

# Command:
```shell
docker build -f docker/Dockerfile .
docker build -f docker/DebugDockerfile .
docker build -f docker/NotFromScrachDockerfile .
```

# docker.io
This solution works also when you want to build your fork using docker hub (docker.io) (and share/test your image directly from there).     

For automating docker build. Make sure you pass the context to root of it project.     
Same for for docker hub (docker.io) builds:      
set the **Dockerfile** column to `docker/DebugDockerfile` and **context** column to `/`     


