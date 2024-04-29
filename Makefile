.PHONY: build run deploy

build:
	@docker build -t pocketbase-litestream .

run:
	@docker run -it --env-file .auth.env -p 8080:8080 pocketbase-litestream

deploy:
	@fly deploy --local-only
