.DEFAULT_GOAL := help
.PHONY: help start stop status logs host-start host-stop

PS := powershell.exe -NoProfile -ExecutionPolicy Bypass

help:
	@echo make start      - start database, app and n8n - the AI agent then runs every 15 minutes
	@echo make stop       - stop everything so no AI API tokens are used
	@echo make status     - show which containers are running
	@echo make logs       - follow the app log - press Ctrl+C to quit
	@echo make host-start - resume the hosted Render service
	@echo make host-stop  - suspend the hosted Render service

start:
	$(PS) -File scripts/start.ps1

stop:
	$(PS) -File scripts/stop.ps1

status:
	docker compose ps

logs:
	docker compose logs -f app

host-start:
	$(PS) -File scripts/render-control.ps1 -Action resume

host-stop:
	$(PS) -File scripts/render-control.ps1 -Action suspend
