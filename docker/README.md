# Developers
[developer docs](docs/developer.md)

[build docs](../BUILD.md)

# Build both code and image using Dockerfile
Most Dockerfiles both build the code as well as the image.

# Choose the desired build flavour:
- WithBuildDockerfile -> builds code as well as more secure a version of the image (from scratch instead of basing on alpine)
- WithBuildDebugDockerfile -> builds a debug version of the image, based on alpine and uses "github.com/derekparker/delve/cmd/dlv". exposes port :7777
- NotFromScrachDockerfile -> alpine based image (notFromScratch like in Dockerfile )
- Dockerfile -> alpine based image, without the build, it expects binary is build outside. This is used by Makefile (& Travis)
- DebugDockerfile -> alpine based image, without the build, it expects binary is build outside. This is used by Makefile (& Travis) to make debug image.

# Command example:
```shell
docker build -f docker/WithBuildDockerfile .
```

# docker.io
This solution works also when you want to build your fork using docker hub (docker.io) (and share/test your image directly from there).     

For automating docker build. Make sure you pass the context to root of it project.     
Same for for docker hub (docker.io) builds:      
set the **Dockerfile** column to `docker/WithBuildDockerfile` and **context** column to `/`     


