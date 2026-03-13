import React from 'react';
import ComponentCreator from '@docusaurus/ComponentCreator';

export default [
  {
    path: '/__docusaurus/debug',
    component: ComponentCreator('/__docusaurus/debug', '5ff'),
    exact: true
  },
  {
    path: '/__docusaurus/debug/config',
    component: ComponentCreator('/__docusaurus/debug/config', '5ba'),
    exact: true
  },
  {
    path: '/__docusaurus/debug/content',
    component: ComponentCreator('/__docusaurus/debug/content', 'a2b'),
    exact: true
  },
  {
    path: '/__docusaurus/debug/globalData',
    component: ComponentCreator('/__docusaurus/debug/globalData', 'c3c'),
    exact: true
  },
  {
    path: '/__docusaurus/debug/metadata',
    component: ComponentCreator('/__docusaurus/debug/metadata', '156'),
    exact: true
  },
  {
    path: '/__docusaurus/debug/registry',
    component: ComponentCreator('/__docusaurus/debug/registry', '88c'),
    exact: true
  },
  {
    path: '/__docusaurus/debug/routes',
    component: ComponentCreator('/__docusaurus/debug/routes', '000'),
    exact: true
  },
  {
    path: '/docs',
    component: ComponentCreator('/docs', '542'),
    routes: [
      {
        path: '/docs',
        component: ComponentCreator('/docs', '91d'),
        routes: [
          {
            path: '/docs',
            component: ComponentCreator('/docs', 'eb6'),
            routes: [
              {
                path: '/docs/api/habitat-api',
                component: ComponentCreator('/docs/api/habitat-api', '420'),
                exact: true
              },
              {
                path: '/docs/api/network-habitat-internal-notify-of-update',
                component: ComponentCreator('/docs/api/network-habitat-internal-notify-of-update', 'a1f'),
                exact: true
              },
              {
                path: '/docs/api/network-habitat-permissions-add-permission',
                component: ComponentCreator('/docs/api/network-habitat-permissions-add-permission', 'bc1'),
                exact: true
              },
              {
                path: '/docs/api/network-habitat-permissions-remove-permission',
                component: ComponentCreator('/docs/api/network-habitat-permissions-remove-permission', '2a3'),
                exact: true
              },
              {
                path: '/docs/api/network-habitat-repo-delete-record',
                component: ComponentCreator('/docs/api/network-habitat-repo-delete-record', 'b22'),
                exact: true
              },
              {
                path: '/docs/api/network-habitat-repo-put-record',
                component: ComponentCreator('/docs/api/network-habitat-repo-put-record', 'ee9'),
                exact: true
              },
              {
                path: '/docs/api/network-habitat-repo-upload-blob',
                component: ComponentCreator('/docs/api/network-habitat-repo-upload-blob', 'b99'),
                exact: true
              }
            ]
          }
        ]
      }
    ]
  },
  {
    path: '/',
    component: ComponentCreator('/', 'e5f'),
    exact: true
  },
  {
    path: '*',
    component: ComponentCreator('*'),
  },
];
