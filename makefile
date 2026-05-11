SHELL := powershell.exe
.SHELLFLAGS := -NoProfile -Command

doc:
	docker-compose -f docker-compose.yml up -d

ml-start:
	cd realval; .\.venv\Scripts\Activate.ps1; $$env:PYTHONPATH='.\src\'; make pr_serve

go-start:
	cd go_back; make run

ui-start:
	cd ui; npm run dev