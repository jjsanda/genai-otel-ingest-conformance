# LangGraph demo: uv-managed Python on slim, running as a non-root user.
FROM python:3.12-slim
COPY --from=ghcr.io/astral-sh/uv:0.10 /uv /uvx /usr/local/bin/

ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy
WORKDIR /app

COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-install-project --no-dev

COPY src/ src/
RUN uv sync --frozen --no-dev

ENV PATH="/app/.venv/bin:$PATH"
# Numeric UID, not the user name: Kubernetes can only verify runAsNonRoot
# against a numeric UID in the image metadata.
RUN useradd --create-home --uid 1000 demo
USER 1000

CMD ["python", "-m", "langgraph_demo.main"]
