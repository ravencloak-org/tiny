# Build the static marketing site with bun, serve it with nginx.
FROM oven/bun:1-alpine AS build
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY . .
RUN bun run build      # next.config output:"export" -> /app/out

FROM nginx:alpine
COPY --from=build /app/out /usr/share/nginx/html
EXPOSE 80
