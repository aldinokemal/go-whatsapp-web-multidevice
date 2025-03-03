export default {
    name: 'AccountContact',
    data() {
        return {
            contacts: []
        }
    },
    methods: {
        async openModal() {
            try {
                this.dtClear()
                await this.submitApi();
                $('#modalContactList').modal('show');
                this.dtRebuild()
                showSuccessInfo("Contacts fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        dtClear() {
            $('#account_contacts_table').DataTable().destroy();
        },
        dtRebuild() {
            $('#account_contacts_table').DataTable({
                "pageLength": 10,
                "reloadData": true,
            }).draw();
        },
        async submitApi() {
            try {
                let response = await window.http.get(`/user/my/contacts`)
                this.contacts = response.data.results.data;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            }
        },
        getPhoneNumber(jid) {
            return jid.split('@')[0];
        }
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui olive right ribbon label">Contacts</a>
            <div class="header">My Contacts</div>
            <div class="description">
                Display all your contacts
            </div>
        </div>
    </div>
    
    <!--  Modal Contact List  -->
    <div class="ui large modal" id="modalContactList">
        <i class="close icon"></i>
        <div class="header">
            My Contacts
        </div>
        <div class="content">
            <table class="ui celled table" id="account_contacts_table">
                <thead>
                <tr>
                    <th>Phone Number</th>
                    <th>Name</th>
                </tr>
                </thead>
                <tbody v-if="contacts != null">
                <tr v-for="contact in contacts">
                    <td>{{ getPhoneNumber(contact.jid) }}</td>
                    <td>{{ contact.name }}</td>
                </tr>
                </tbody>
            </table>
        </div>
    </div>
    `
}
