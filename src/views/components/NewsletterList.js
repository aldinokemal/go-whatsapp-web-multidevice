export default {
    name: 'ListNewsletter',
    data() {
        return {
            newsletters: []
        }
    },
    methods: {
        async openModal() {
            try {
                this.dtClear()
                await this.submitApi();
                $('#modalNewsletterList').modal('show');
                this.dtRebuild()
                showSuccessInfo("Newsletters fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        dtClear() {
            $('#account_newsletters_table').DataTable().destroy();
        },
        dtRebuild() {
            $('#account_newsletters_table').DataTable({
                "pageLength": 100,
                "reloadData": true,
            }).draw();
        },
        async handleUnfollowNewsletter(newsletter_id) {
            try {
                const ok = confirm("Are you sure to leave this newsletter?");
                if (!ok) return;

                await this.unfollowNewsletterApi(newsletter_id);
                this.dtClear()
                await this.submitApi();
                this.dtRebuild()
                showSuccessInfo("Success unfollow newsletter")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async unfollowNewsletterApi(newsletter_id) {
            try {
                let payload = {
                    newsletter_id: newsletter_id
                };
                await window.http.post(`/newsletter/unfollow`, payload)
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);

            }
        },
        async submitApi() {
            try {
                let response = await window.http.get(`/user/my/newsletters`)
                this.newsletters = response.data.results.data;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            }
        },
        formatDate: function (value) {
            if (!value) return ''
            if (isNaN(value)) return 'Invalid date';
            return moment.unix(value).format('LLL');
        }
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Newsletter</a>
            <div class="header">List Newsletters</div>
            <div class="description">
                Display all your newsletters
            </div>
        </div>
    </div>
    
    <!--  Modal AccountNewsletter  -->
    <div class="ui small modal" id="modalNewsletterList">
        <i class="close icon"></i>
        <div class="header">
            My Newsletter List
        </div>
        <div class="content">
            <table class="ui celled table" id="account_newsletters_table">
                <thead>
                <tr>
                    <th>Newsletter ID</th>
                    <th>Name</th>
                    <th>Role</th>
                    <th>Created At</th>
                    <th>Action</th>
                </tr>
                </thead>
                <tbody v-if="newsletters != null">
                <tr v-for="n in newsletters">
                    <td>{{ n.id.split('@')[0] }}</td>
                    <td>{{ n.thread_metadata?.name?.text || 'N/A' }}</td>
                    <td>{{ n.viewer_metadata?.role || 'N/A' }}</td>
                    <td>{{ formatDate(n.thread_metadata?.creation_time) }}</td>
                    <td>
                        <button class="ui red tiny button" @click="handleUnfollowNewsletter(n.id)">Unfollow</button>
                    </td>
                </tr>
                </tbody>
            </table>
        </div>
    </div>
    `
}