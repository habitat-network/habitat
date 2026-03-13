import type { SidebarsConfig } from "@docusaurus/plugin-content-docs";

const sidebar: SidebarsConfig = {
  apisidebar: [
    {
      type: "doc",
      id: "api/habitat-api",
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
          id: "api/network-habitat-repo-delete-record",
          label: "network.habitat.repo.deleteRecord",
          className: "api-method post",
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
