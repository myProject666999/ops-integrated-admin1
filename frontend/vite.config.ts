import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

function normalizeBaseURL(raw: string): string {
  return String(raw || '').trim().replace(/\/+$/, '')
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd())
  const apiTarget = normalizeBaseURL(env.VITE_API_BASE || 'http://localhost:8080')
  const isProd = mode === 'production'

  return {
    plugins: [vue()],
    base: '/',
    resolve: {
      alias: {
        '@': resolve(__dirname, 'src')
      }
    },
    build: {
      outDir: resolve(__dirname, 'dist'),
      emptyOutDir: true,
      // 优化构建性能
      target: 'es2015',
      cssTarget: 'chrome61',
      // 代码分割优化
      rollupOptions: {
        output: {
          // 手动分块策略
          manualChunks: {
            // 将naive-ui单独打包
            'naive-ui': ['naive-ui'],
            // 将vue核心单独打包
            'vue-core': ['vue', 'vue-router', 'pinia'],
          },
          // 确保资源文件使用相对路径
          entryFileNames: 'assets/[name]-[hash].js',
          chunkFileNames: 'assets/[name]-[hash].js',
          assetFileNames: (assetInfo) => {
            const info = assetInfo.name || ''
            if (info.endsWith('.css')) {
              return 'assets/[name]-[hash][extname]'
            }
            return 'assets/[name]-[hash][extname]'
          },
        },
      },
      // 启用CSS代码分割
      cssCodeSplit: true,
      // 启用源码映射（生产环境可关闭）
      sourcemap: !isProd,
      // 压缩选项
      minify: isProd ? 'esbuild' : false,
      // 报告压缩后大小
      reportCompressedSize: isProd,
    },
    server: {
      host: '0.0.0.0',
      port: 3000,
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
          ws: true
        }
      }
    },
    // 优化依赖预构建
    optimizeDeps: {
      include: [
        'vue',
        'vue-router',
        'pinia',
        'naive-ui',
        'axios',
        'echarts',
      ],
    },
    // CSS配置
    css: {
      devSourcemap: !isProd,
    },
  }
})
