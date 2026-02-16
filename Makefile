build:
	go install ./...

.ALWAYS:

test: .ALWAYS
	bash -c "\
	if ! which systemctl >/dev/null 2>&1; then \
		export PATH=/Users/john/proj/shiftwrap/scripts:${PATH}; \
	fi; \
	go test -v ./..."
