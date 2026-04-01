import type { SidebarsConfig } from '@docusaurus/plugin-content-docs';
import apiSidebar from './docs/api/sidebar';

const sidebars: SidebarsConfig = {
  docs: [
    {
      type: 'doc',
      id: 'habitat',
    },
    {
      type: 'category',
      label: 'Building on habitat',
      items: [
        'building/auth',
        'building/forwarding',
        'building/permissions',
        'building/sync',
      ],
    },
    {
      type: 'category',
      label: 'Specs',
      items: [
        'specs/pear',
        'specs/cliques'
      ],
    },
    {
      type: 'category',
      label: 'HTTP Reference',
      items: [
        ...apiSidebar,
      ],
    }
  ]
};

export default sidebars;