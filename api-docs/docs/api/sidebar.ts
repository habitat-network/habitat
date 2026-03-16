import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api/habitat-api",
    },
    {
      type: "category",
      label: "network.habitat.clique",
      items: [
        {
          type: "doc",
          id: "api/network-habitat-clique-add-members",
          label: "network.habitat.clique.addMembers",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/network-habitat-clique-create-clique",
          label: "network.habitat.clique.createClique",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/network-habitat-clique-get-members",
          label: "network.habitat.clique.getMembers",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/network-habitat-clique-is-member",
          label: "network.habitat.clique.isMember",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/network-habitat-clique-remove-members",
          label: "network.habitat.clique.removeMembers",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "network.habitat.listConnectedApps",
      items: [
        {
          type: "doc",
          id: "api/network-habitat-list-connected-apps",
          label: "network.habitat.listConnectedApps",
          className: "api-method get",
        },
      ],
    },
    {
      type: "category",
      label: "network.habitat.internal",
      items: [
        {
          type: "doc",
          id: "api/network-habitat-internal-notify-of-update",
          label: "network.habitat.internal.notifyOfUpdate",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "network.habitat.permissions",
      items: [
        {
          type: "doc",
          id: "api/network-habitat-permissions-add-permission",
          label: "network.habitat.permissions.addPermission",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/network-habitat-permissions-list-permissions",
          label: "network.habitat.permissions.listPermissions",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/network-habitat-permissions-remove-permission",
          label: "network.habitat.permissions.removePermission",
          className: "api-method post",
        },
      ],
    },
    {
      type: "category",
      label: "network.habitat.repo",
      items: [
        {
          type: "doc",
          id: "api/network-habitat-repo-create-record",
          label: "network.habitat.repo.createRecord",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/network-habitat-repo-delete-record",
          label: "network.habitat.repo.deleteRecord",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/network-habitat-repo-get-blob",
          label: "network.habitat.repo.getBlob",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/network-habitat-repo-get-record",
          label: "network.habitat.repo.getRecord",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/network-habitat-repo-list-collections",
          label: "network.habitat.repo.listCollections",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/network-habitat-repo-list-records",
          label: "network.habitat.repo.listRecords",
          className: "api-method get",
        },
        {
          type: "doc",
          id: "api/network-habitat-repo-put-record",
          label: "network.habitat.repo.putRecord",
          className: "api-method post",
        },
        {
          type: "doc",
          id: "api/network-habitat-repo-upload-blob",
          label: "network.habitat.repo.uploadBlob",
          className: "api-method post",
        },
      ],
    },
  ],
};

export default sidebar.apisidebar;
