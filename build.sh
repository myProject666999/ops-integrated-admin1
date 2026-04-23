#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="${SCRIPT_DIR}/frontend"
BACKEND_DIR="${SCRIPT_DIR}/backend"
STATIC_DIR="${BACKEND_DIR}/static"

echo "==> Building frontend..."
cd "${FRONTEND_DIR}"

if [ ! -d "node_modules" ]; then
    echo "==> Installing frontend dependencies..."
    npm install
fi

echo "==> Running vite build..."
npm run build

echo "==> Cleaning static directory..."
rm -rf "${STATIC_DIR}"
mkdir -p "${STATIC_DIR}"

echo "==> Copying built files to backend/static..."
if [ -d "${FRONTEND_DIR}/dist" ]; then
    cp -r "${FRONTEND_DIR}/dist/"* "${STATIC_DIR}/"
    echo "==> Frontend build completed successfully!"
    echo "==> Static files are in: ${STATIC_DIR}"
else
    echo "Error: Frontend build output directory not found: ${FRONTEND_DIR}/dist"
    exit 1
fi

echo ""
echo "==> Build summary:"
echo "    Frontend source: ${FRONTEND_DIR}"
echo "    Build output:    ${FRONTEND_DIR}/dist"
echo "    Copied to:       ${STATIC_DIR}"
echo ""
echo "==> You can now run the backend server to serve both API and static files."
