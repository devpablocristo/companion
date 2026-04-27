FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npx vite build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY docker-entrypoint.d/40-governance-proxy-envsubst.sh /docker-entrypoint.d/40-governance-proxy-envsubst.sh
RUN chmod +x /docker-entrypoint.d/40-governance-proxy-envsubst.sh
EXPOSE 80
