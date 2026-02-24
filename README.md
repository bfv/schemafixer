# go-cli-template
A template repo for a Go CLI project 

## installation

`github/workflows/release.yaml` contains there placeholders:
```
env:
  DOCKER_IMAGE: devbfvio/<< project_name >>
  EXE_NAME: << project_name >>
  IMAGE_TITLE: << todo >>
  IMAGE_DESCRIPTION: << todo >>
  IMAGE_LICENSES: MIT
  IMAGE_AUTHORS: Bronco Oostermeyer <dev@bfv.io>
```

These can be set via the `init.sh` script. For Windows, use either WSL2 or Git bash.

## Docker hub integration
`release.yaml` is configured to push a Docker image upon tagging with `vx.y.z`
Set the value in Github:
```
# dont forget to set these repository secrets and variables:
#   - DOCKERHUB_USERNAME
#   - DOCKERHUB_TOKEN
```

