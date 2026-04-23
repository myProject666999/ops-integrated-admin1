# Windows Dockerfile for ops-admin
# 使用Windows Server Core作为基础镜像

FROM mcr.microsoft.com/windows/servercore:ltsc2022

SHELL ["powershell", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]

WORKDIR C:/app

# 复制预编译的Windows后端可执行文件
COPY backend/ops-admin-backend.exe ./
COPY backend/data ./data
# 使用本地已构建的前端dist目录
COPY frontend/dist ./static

# 创建数据库目录
RUN New-Item -ItemType Directory -Force -Path C:/app/db

ENV ADDR=0.0.0.0:8080
ENV TZ=Asia/Shanghai
ENV STATIC_DIR=./static

EXPOSE 8080

CMD ["ops-admin-backend.exe"]
