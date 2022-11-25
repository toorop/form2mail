build:
	go build -o dist/form2mail

deploy: build
	rsync -vz dist/* root@dpp.st:/var/www/form2mail/
	ssh root@dpp.st systemctl restart form2mail.service