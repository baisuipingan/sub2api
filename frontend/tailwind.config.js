/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // 主色调 - 电光蓝（A+ 方案）
        // 用途：当前选中态、唯一主操作。全屏面积控制在 10% 以内。
        primary: {
          50: '#eff6ff',
          100: '#dbeafe',
          200: '#bfdbfe',
          300: '#93c5fd',
          400: '#60a5fa',
          500: '#3b82f6',
          600: '#2563eb',
          700: '#1d4ed8',
          800: '#1e40af',
          900: '#1e3a8a',
          950: '#172554'
        },
        // 中性色 - 带极轻冷调（chroma 0.006/250），避免纯灰的死板
        accent: {
          50: '#f8f9fb',
          100: '#f1f3f6',
          200: '#e5e8ed',
          300: '#d0d4dc',
          400: '#9ba1ad',
          500: '#6d747f',
          600: '#515762',
          700: '#3b404a',
          800: '#272b33',
          900: '#181b21',
          950: '#0d0f13'
        },
        // 深色模式背景
        dark: {
          50: '#f8f9fb',
          100: '#f1f3f6',
          200: '#e5e8ed',
          300: '#d0d4dc',
          400: '#9ba1ad',
          500: '#6d747f',
          600: '#515762',
          700: '#3b404a',
          800: '#272b33',
          900: '#181b21',
          950: '#0d0f13'
        }
      },
      fontFamily: {
        sans: [
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 8px 32px rgba(0, 0, 0, 0.06)',
        'glass-sm': '0 4px 16px rgba(0, 0, 0, 0.04)',
        // 极轻的边框替身，A+ 方案基本不用阴影
        glow: '0 0 0 3px rgba(37, 99, 235, 0.14)',
        'glow-lg': '0 0 0 4px rgba(37, 99, 235, 0.18)',
        card: '0 1px 2px rgba(16, 24, 40, 0.04)',
        'card-hover': '0 2px 8px rgba(16, 24, 40, 0.07)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #2563eb 0%, #1d4ed8 100%)',
        'gradient-dark': 'linear-gradient(135deg, #272b33 0%, #181b21 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 20%, rgba(37, 99, 235, 0.10) 0px, transparent 50%), radial-gradient(at 80% 0%, rgba(59, 130, 246, 0.07) 0px, transparent 50%), radial-gradient(at 0% 50%, rgba(37, 99, 235, 0.06) 0px, transparent 50%)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 0 3px rgba(37, 99, 235, 0.12)' },
          '100%': { boxShadow: '0 0 0 4px rgba(37, 99, 235, 0.20)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '2rem'
      }
    }
  },
  plugins: []
}
