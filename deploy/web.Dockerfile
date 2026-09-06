# Static frontend tier: nginx serving the built SPA. In the split k8s
# deployment (k3s-home apps/offerlog) the ingress routes / to this tier's
# Service (offerlog-web) and /api straight to the api Service, so frontend
# changes deploy independently of the api image — frontend/Jenkinsfile builds
# & pushes this image on main and its gitops stage bumps
# apps/offerlog/web-deployment.yaml. The compose single-entry deployment
# instead serves the SPA from the api image (deploy/api.Dockerfile); its
# nginx config (nginx.web.conf) still proxies /api for that mode.
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
