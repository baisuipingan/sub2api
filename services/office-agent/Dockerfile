ARG PYTHON_BASE_IMAGE
FROM ${PYTHON_BASE_IMAGE}
ARG PYTHON_BASE_IMAGE

ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    LANGSMITH_TRACING=false LANGCHAIN_TRACING_V2=false
WORKDIR /app
COPY requirements.lock ./
RUN case "$PYTHON_BASE_IMAGE" in *@sha256:*) ;; *) exit 1 ;; esac \
    && python -m pip install --no-cache-dir --require-hashes -r requirements.lock \
    && groupadd --gid 11001 office \
    && useradd --uid 11001 --gid office --no-create-home --home-dir /nonexistent office
COPY pyproject.toml ./
COPY src ./src
COPY vendor ./vendor
RUN python -m pip install --no-cache-dir --no-deps . \
    && rm -rf /app/src /app/vendor
USER 11001:11001
CMD ["office-agent"]
