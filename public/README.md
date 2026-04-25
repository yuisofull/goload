# GoLoad Frontend

A modern React frontend for the GoLoad download manager.

## Features

- HTTP/HTTPS/FTP downloads
- BitTorrent support (magnet links & .torrent files)
- JWT authentication
- Real-time download progress
- Responsive design with Tailwind CSS

## Docker

Build and run with Docker:

```bash
# Build the image
docker build -t goload-frontend .

# Run the container
docker run -p 8080:80 goload-frontend
```

The app will be available at http://localhost:8080

## Development

```bash
# Install dependencies
bun install

# Start development server
bun run dev

# Build for production
bun run build
```

## Environment Variables

- `VITE_GOLOAD_API_URL` - Backend API URL (default: http://localhost:8080)
