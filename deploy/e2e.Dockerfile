# E2E runner image: Playwright browsers come preinstalled in the base image
# (PLAYWRIGHT_BROWSERS_PATH=/ms-playwright); keep the npm playwright version
# pinned to the same release as the base image tag.
FROM mcr.microsoft.com/playwright:v1.59.1-noble

WORKDIR /work
COPY scripts/package*.json ./
RUN npm ci
COPY scripts/*.cjs ./

# E2E_BASE / E2E_EMAIL / E2E_PASSWORD are provided by compose.test.yaml
CMD ["node", "e2e.cjs"]
