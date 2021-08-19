# Makefile to build an execute tests

tests: build-as

build-as:
	@echo "Building AssemblyScript Testdata"
	docker run -v `pwd`/testdata/as:/usr/app/ asc hello.ts -b hello.wasm --config tsconfig.json 
