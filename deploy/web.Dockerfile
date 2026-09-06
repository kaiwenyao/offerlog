# Optional static-only web tier: nginx serving the built SPA, proxying /api
# and /health to the api service (compose) or an upstream (k8s).
# The DEFAULT deployment does not use this image — deploy/api.Dockerfile
# serves the SPA from the api binary itself. This image exists for split-tier
# setups (e.g. scale the web tier independently behind an ingress).
# Built & pushed by frontend/Jenkinsfile on main.
FROM node:22-alpine AS fe
WORKDIR /fe
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM nginx:1.27-alpine
# The official nginx entrypoint envsubst's /etc/nginx/templates/*.template —
# ${API_UPSTREAM} is replaced (nginx runtime vars like $host stay literal).
ENV API_UPSTREAM=api:8080
COPY deploy/nginx.web.conf /etc/nginx/templates/default.conf.template
COPY --from=fe /fe/dist /usr/share/nginx/html
EXPOSE 8080
