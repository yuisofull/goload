FROM oven/bun:1

WORKDIR /app

# Copy package files
COPY package.json bun.lockb ./

# Install dependencies
RUN bun install --frozen-lockfile

# Copy source code
COPY . .

ARG VITE_GOLOAD_API_URL
ENV VITE_GOLOAD_API_URL=$VITE_GOLOAD_API_URL

CMD ["bun", "run", "build"]