all: push

build:
	go build

push:
	git add .
	git commit -am "ok"
	git push -u origin master
