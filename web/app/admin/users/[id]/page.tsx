import { UserDetail } from "../../operations";

export default async function Page({ params }: PageProps<"/admin/users/[id]">) {
  return <UserDetail id={(await params).id} />;
}
