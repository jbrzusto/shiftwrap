build:
	go install ./...

.ALWAYS:

test: .ALWAYS
	bash -c "\
	if ! which systemctl; then \
		export PATH=/Users/john/proj/shiftwrap/scripts:${PATH}; \
	fi; \
	go test -v ./..."
