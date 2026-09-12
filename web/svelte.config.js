import adapter from '@sveltejs/adapter-static';

/** @type {import('@sveltejs/kit').Config} */
const config = {
  kit: {
    serviceWorker: {
      register: false
    },
    adapter: adapter({
      fallback: 'index.html',
      pages: '../internal/webui/dist',
      assets: '../internal/webui/dist',
      precompress: false,
      strict: true
    })
  }
};

export default config;
