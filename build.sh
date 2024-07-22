TAG=$(git rev-parse --short HEAD)
docker build . -t paulgmiller/aggrovites:$TAG
docker push paulgmiller/aggrovites:$TAG
