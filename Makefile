.PHONY: build run deploy

build:
	@DOCKER_BUILDKIT=1 docker build -t pocketbase-litestream .

run:
	@docker run -it --env-file .auth.env -v `pwd`/scripts/branched.sh:/scripts/run.sh -p 8080:8080 pocketbase-litestream

deploy:
	@fly deploy --local-only

secrets:
	@cat .auth.env | fly secrets import -
