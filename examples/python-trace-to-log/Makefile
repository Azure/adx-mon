.PHONY: help install run clean

help:
	@echo "Python Trace Example"
	@echo "===================="
	@echo ""
	@echo "Available targets:"
	@echo "  install  - Install Python dependencies"
	@echo "  run      - Run the trace example"
	@echo "  clean    - Remove Python cache files"

install:
	pip install -r requirements.txt

run:
	python trace_example.py

clean:
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -type f -name "*.pyc" -delete
