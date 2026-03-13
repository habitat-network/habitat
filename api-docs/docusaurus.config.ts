import { themes as prismThemes } from 'prism-react-renderer';
import type { Config } from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const baseUrl = process.env.BASE_URL ?? '/';

const config: Config = {
  title: 'Habitat API',
  favicon: 'img/habitat.ico',

  future: {
    v4: true,
  },

  url: 'https://habitat.network',
  baseUrl: baseUrl, // /habitat/api in production, just localhost:3000/ in dev

  organizationName: 'habitat',
  projectName: 'habitat',

  onBrokenLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          docItemComponent: '@theme/ApiItem',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    [
      'docusaurus-plugin-openapi-docs',
      {
        id: 'openapi',
        docsPluginId: 'classic',
        config: {
          habitat: {
            specPath: '../typescript/xrpc-openapi-gen/spec/api.json',
            outputDir: 'docs/api',
            sidebarOptions: { groupPathsBy: 'tag' },
          },
        },
      },
    ],
  ],

  themes: ['docusaurus-theme-openapi-docs'],

  themeConfig: {
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'Habitat API',
      logo: {
        alt: 'Habitat Logo',
        src: 'img/habitat.svg',
      },
      items: [
        {
          to: baseUrl + '/docs/api',
          position: 'left',
          label: 'API Reference',
        },
        {
          href: 'https://github.com/habitat-network/habitat',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {
              label: 'API Reference',
              to: baseUrl + '/docs/api',
            },
          ],
        },
        {
          title: 'More',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/habitat-network/habitat',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Habitat. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
