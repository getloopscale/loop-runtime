FROM node:20-alpine
WORKDIR /app
# Intentional build failure: this command exits 1 during BuildKit build.
# Used to validate LoopRuntime's build failure → exit 2 contract (spec §12).
RUN echo "Simulated build failure for failure-mode test" && false
COPY . .
CMD ["node", "src/index.js"]
