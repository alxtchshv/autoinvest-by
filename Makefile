# AutoInvest — команды для macOS и Linux (на Windows: run.bat, create_demo.bat, create_admin.bat)
.PHONY: help run demo admin build web test vet fmt clean dev-web

help:           ## список команд
	@grep -E '^[a-z-]+:.*## ' Makefile | sed 's/:.*## /\t— /'

web:            ## собрать фронтенд в web/dist
	cd web/app && npm ci --no-audit --no-fund && npm run build

build:          ## собрать бинарник bin/autoinvest (фронтенд — если есть Node.js)
	./scripts/build.sh

run:            ## запустить сервер на http://localhost:8080
	./scripts/run.sh

demo:           ## демо-аккаунт demo@autoinvest.by / demo12345 (только PAYMENTS=sandbox)
	./scripts/run.sh demo

admin:          ## make admin EMAIL=you@mail.by PASS=пароль123
	@test -n "$(EMAIL)" -a -n "$(PASS)" || { echo "Использование: make admin EMAIL=you@mail.by PASS=пароль123"; exit 2; }
	./scripts/run.sh admin "$(EMAIL)" "$(PASS)"

test:           ## все тесты Go
	go test ./internal/...

vet:            ## статический анализ
	go vet ./...

fmt:            ## форматирование Go-кода
	gofmt -w cmd internal web

clean:          ## удалить бинарники (данные в data/ не трогаются)
	rm -rf bin

dev-web:        ## фронтенд с горячей перезагрузкой на :5173 (бэкенд должен работать на :8080)
	cd web/app && npm run dev
