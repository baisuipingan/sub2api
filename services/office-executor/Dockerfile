# Build context: repository root. Release builds must supply a reviewed digest:
# docker build --build-arg PYTHON_BASE_IMAGE=python:3.12-slim-bookworm@sha256:... \
#   -f services/office-executor/Dockerfile -t milo-office-task:<revision> .
# No default base deliberately: do not silently track a mutable base on release.
ARG PYTHON_BASE_IMAGE
FROM ${PYTHON_BASE_IMAGE}

ARG PYTHON_BASE_IMAGE
RUN case "$PYTHON_BASE_IMAGE" in *@sha256:*) ;; *) exit 1 ;; esac

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    PYTHONPATH=/opt/office-executor \
    MPLBACKEND=Agg \
    MATPLOTLIBRC=/etc/matplotlibrc

RUN apt-get update && apt-get install -y --no-install-recommends \
        libreoffice-writer libreoffice-calc libreoffice-impress libreoffice-java-common \
        default-jre-headless poppler-utils pandoc fonts-noto-cjk fonts-liberation fontconfig tini \
    && fc-cache -f && rm -rf /var/lib/apt/lists/*

COPY services/office-executor/requirements-task.txt /opt/office-executor/requirements-task.txt
RUN pip install --no-cache-dir --only-binary=:all: -r /opt/office-executor/requirements-task.txt \
    && pip check \
    && python -c 'import docx, pptx, openpyxl, pandas, matplotlib, PIL, pypdf, lxml, reportlab, numpy'

COPY services/office-executor/office_executor /opt/office-executor/office_executor
COPY services/office-agent/vendor/bisheng/skills /opt/office-skills
COPY services/office-agent/vendor/bisheng/LICENSE /opt/office-skills/LICENSE.bisheng
COPY services/office-agent/vendor/bisheng/SOURCE_MANIFEST.json /opt/office-skills/SOURCE_MANIFEST.json
RUN chmod -R a-w /opt/office-executor /opt/office-skills \
    && printf '%s\n' 'font.family: sans-serif' 'font.sans-serif: Noto Sans CJK SC, DejaVu Sans' 'axes.unicode_minus: False' > /etc/matplotlibrc \
    && mkdir -p /workspace/uploads /run/office-input \
    && chmod 0555 /workspace /run/office-input

USER 65532:65532
WORKDIR /workspace
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["python", "-m", "office_executor.runner"]
