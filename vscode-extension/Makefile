.PHONY: build clean install

build: node_modules
	npx tsc -p ./
	npx @vscode/vsce package --no-dependencies -o devlake-workflow-editor.vsix

node_modules: package.json
	npm install

clean:
	rm -rf out node_modules devlake-workflow-editor.vsix

install: build
	code --install-extension devlake-workflow-editor.vsix
