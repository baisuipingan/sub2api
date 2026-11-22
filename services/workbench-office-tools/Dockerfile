# Build using the reviewed base image digest configured for the test stack.
ARG PYTHON_BASE_IMAGE=python:3.12-slim-bookworm
FROM ${PYTHON_BASE_IMAGE}
ARG OFFICE_TOOLS_VERSION=1
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 LANG=C.UTF-8 OFFICE_TOOLS_VERSION=${OFFICE_TOOLS_VERSION}
WORKDIR /app
COPY requirements.txt /app/
RUN apt-get update \
    && apt-get install -y --no-install-recommends libreoffice-calc libreoffice-writer libseccomp2 fonts-liberation fonts-noto-cjk \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir --only-binary=:all: -r requirements.txt \
    && pip check \
    && mkdir -p /var/lib/office-tools \
    && chown 65532:65532 /var/lib/office-tools
COPY workbench_office_tools/ /app/workbench_office_tools/
USER 65532:65532
EXPOSE 8094
CMD ["python", "-m", "workbench_office_tools.server"]
