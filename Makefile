.DEFAULT_GOAL := help
.PHONY: help start stop host-start host-stop

help:
	@echo Available commands: make start, make stop, make host-start, make host-stop
	@echo make stop also suspends the hosted Render service.

start:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/start.ps1

stop:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/render-control.ps1 -Action suspend
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/stop.ps1

host-start:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/render-control.ps1 -Action resume

host-stop:
	powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/render-control.ps1 -Action suspend
